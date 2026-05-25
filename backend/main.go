package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mmcdole/gofeed"
)

type News struct {
	ID           int       `json:"id"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	ImageURL     string    `json:"image_url"`
	PublisherURL string    `json:"publisher_url"`
	CreatedAt    time.Time `json:"created_at"`
}

var pool *pgxpool.Pool

func main() {
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		log.Println("DATABASE_URL is not set, skipping DB connection for now")
	} else {
		// Connect in the background to allow the server to start and pass health checks
		go func() {
			var lastErr error
			for i := 0; i < 30; i++ {
				currURL := dbURL

				// Ensure sslmode is set correctly based on attempt number and URL type
				u, err := url.Parse(currURL)
				if err == nil {
					q := u.Query()
					if i >= 10 && strings.Contains(u.Host, ".render.com") {
						// After 10 failed attempts, try internal host and disable SSL
						hostParts := strings.Split(u.Host, ".")
						if len(hostParts) > 1 {
							u.Host = hostParts[0]
							q.Set("sslmode", "disable")
						}
					} else if q.Get("sslmode") == "" || q.Get("sslmode") == "disable" {
						// Default to require for Render Postgres external connections
						q.Set("sslmode", "require")
					}
					u.RawQuery = q.Encode()
					currURL = u.String()
				}

				config, err := pgxpool.ParseConfig(currURL)
				if err == nil {
					if config.ConnConfig.TLSConfig == nil {
						config.ConnConfig.TLSConfig = &tls.Config{}
					}
					config.ConnConfig.TLSConfig.InsecureSkipVerify = true
					config.ConnConfig.TLSConfig.ServerName = config.ConnConfig.Host

					p, err := pgxpool.NewWithConfig(context.Background(), config)
					if err == nil {
						err = p.Ping(context.Background())
						if err == nil {
							pool = p
							log.Printf("Successfully connected to the database using %s", maskPassword(currURL))
							break
						}
						p.Close()
					}
					lastErr = err
				} else {
					lastErr = err
				}

				log.Printf("Failed to connect to database (attempt %d) using %s: %v", i+1, maskPassword(currURL), lastErr)
				time.Sleep(10 * time.Second)
			}

			if pool == nil {
				log.Printf("Could not connect to database after many retries")
				return
			}

			// Auto-migrate: Create table
			_, err := pool.Exec(context.Background(), `
				CREATE TABLE IF NOT EXISTS news (
					id SERIAL PRIMARY KEY,
					title TEXT NOT NULL,
					content TEXT,
					image_url TEXT,
					publisher_url TEXT UNIQUE,
					created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);
			`)
			if err != nil {
				log.Printf("Failed to create table: %v", err)
				return
			}
			log.Println("Database schema initialized.")

			// Start background scraper
			startScheduledScraper()
		}()
	}

	http.HandleFunc("/api/news", getNews)
	http.HandleFunc("/api/health", healthCheck)

	port := os.Getenv("PORT")
	if port == "" {
		port = "10000" // Use 10000 for Render
	}
	fmt.Printf("Server starting on port %s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func maskPassword(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "invalid-url"
	}
	if u.User != nil {
		_, hasPassword := u.User.Password()
		if hasPassword {
			u.User = url.UserPassword(u.User.Username(), "****")
		}
	}
	return u.String()
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	status := "OK"
	dbStatus := "Connected"
	if pool == nil {
		dbStatus = "Connecting or Not Configured"
	} else if err := pool.Ping(context.Background()); err != nil {
		dbStatus = "Disconnected: " + err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   status,
		"database": dbStatus,
		"time":     time.Now().Format(time.RFC3339),
	})
}

func startScheduledScraper() {
	log.Println("Starting background scraper scheduler...")
	// Run immediately on start
	go runScraper()

	// Then run every hour
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			runScraper()
		}
	}()
}

func getNews(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if pool == nil {
		json.NewEncoder(w).Encode([]News{})
		return
	}

	rows, err := pool.Query(context.Background(), "SELECT id, title, COALESCE(content, ''), COALESCE(image_url, ''), publisher_url, created_at FROM news ORDER BY created_at DESC LIMIT 20")
	if err != nil {
		log.Println("Query error:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	newsList := []News{}
	for rows.Next() {
		var n News
		err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.ImageURL, &n.PublisherURL, &n.CreatedAt)
		if err != nil {
			log.Println("Scan error:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		newsList = append(newsList, n)
	}

	json.NewEncoder(w).Encode(newsList)
}

func runScraper() {
	log.Println("Scraping cycle started...")
	fp := gofeed.NewParser()

	// Soccer news in Amharic (እግር ኳስ)
	amharicQuery := url.QueryEscape("እግር ኳስ")
	feedURL := fmt.Sprintf("https://news.google.com/rss/search?q=%s&hl=am&gl=ET&ceid=ET:am", amharicQuery)

	feed, err := fp.ParseURL(feedURL)
	if err != nil || len(feed.Items) == 0 {
		log.Println("Amharic feed failed or empty, trying English soccer news:", err)
		feed, err = fp.ParseURL("https://news.google.com/rss/search?q=soccer&hl=en-US&gl=US&ceid=US:en")
		if err != nil {
			log.Println("English feed also failed:", err)
			return
		}
	}

	log.Printf("Found %d items in RSS feed.", len(feed.Items))
	for _, item := range feed.Items {
		scrapeAndStore(item)
	}
	log.Println("Scraping cycle completed.")
}

func scrapeAndStore(item *gofeed.Item) {
	if pool == nil {
		return
	}

	var existingID int
	err := pool.QueryRow(context.Background(), "SELECT id FROM news WHERE publisher_url = $1", item.Link).Scan(&existingID)
	if err == nil {
		return
	}

	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"),
	)
	var content string
	var imageURL string

	c.OnHTML("article", func(e *colly.HTMLElement) {
		if content == "" {
			content = strings.TrimSpace(e.Text)
		}
	})

	c.OnHTML("meta[property='og:image']", func(e *colly.HTMLElement) {
		if imageURL == "" {
			imageURL = e.Attr("content")
		}
	})

	c.OnHTML("p", func(e *colly.HTMLElement) {
		if len(content) < 2000 {
			content += strings.TrimSpace(e.Text) + " "
		}
	})

	err = c.Visit(item.Link)
	if err != nil {
		log.Println("Error scraping:", item.Link, err)
	}

	if content == "" {
		content = strings.TrimSpace(item.Description)
	}

	_, err = pool.Exec(context.Background(),
		"INSERT INTO news (title, content, image_url, publisher_url, created_at) VALUES ($1, $2, $3, $4, $5)",
		item.Title, content, imageURL, item.Link, time.Now())
	if err != nil {
		log.Println("Error inserting into DB:", err)
	} else {
		log.Println("Stored news:", item.Title)
	}
}

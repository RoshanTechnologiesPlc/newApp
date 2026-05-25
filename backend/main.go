package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/jackc/pgx/v5"
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

var db *pgx.Conn

func main() {
	// Initialize DB connection with retries
	var err error
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Println("DATABASE_URL is not set, skipping DB connection for now")
	} else {
		// Ensure sslmode=require for Render Postgres
		if !strings.Contains(dbURL, "sslmode=") {
			if strings.Contains(dbURL, "?") {
				dbURL += "&sslmode=require"
			} else {
				dbURL += "?sslmode=require"
			}
		}

		for i := 0; i < 5; i++ {
			db, err = pgx.Connect(context.Background(), dbURL)
			if err == nil {
				break
			}
			log.Printf("Failed to connect to database (attempt %d): %v", i+1, err)
			time.Sleep(5 * time.Second)
		}

		if err != nil {
			log.Fatal("Could not connect to database after retries:", err)
		}
		defer db.Close(context.Background())

		// Auto-migrate: Create table
		_, err = db.Exec(context.Background(), `
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
			log.Fatal("Failed to create table:", err)
		}
		log.Println("Database schema initialized.")
	}

	// Start background scraper
	if db != nil {
		go startScheduledScraper()
	}

	http.HandleFunc("/api/news", getNews)
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Server starting on port %s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func startScheduledScraper() {
	// Run immediately on start
	runScraper()

	// Then run every hour
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		runScraper()
	}
}

func getNews(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if db == nil {
		json.NewEncoder(w).Encode([]News{})
		return
	}

	rows, err := db.Query(context.Background(), "SELECT id, title, COALESCE(content, ''), COALESCE(image_url, ''), publisher_url, created_at FROM news ORDER BY created_at DESC LIMIT 20")
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
	log.Println("Starting scraper...")
	fp := gofeed.NewParser()

	// Trying soccer news in Amharic first
	feed, err := fp.ParseURL("https://news.google.com/rss/search?q=soccer&hl=am&gl=ET&ceid=ET:am")
	if err != nil {
		log.Println("Amharic feed failed, trying English:", err)
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
	var existingID int
	err := db.QueryRow(context.Background(), "SELECT id FROM news WHERE publisher_url = $1", item.Link).Scan(&existingID)
	if err == nil {
		// Already exists
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

	_, err = db.Exec(context.Background(),
		"INSERT INTO news (title, content, image_url, publisher_url, created_at) VALUES ($1, $2, $3, $4, $5)",
		item.Title, content, imageURL, item.Link, time.Now())
	if err != nil {
		log.Println("Error inserting into DB:", err)
	} else {
		log.Println("Stored news:", item.Title)
	}
}

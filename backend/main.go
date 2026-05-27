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
	"regexp"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mmcdole/gofeed"
)

// ─── Data Types ──────────────────────────────────────────────────────────────

type News struct {
	ID           int       `json:"id"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	ImageURL     string    `json:"image_url"`
	PublisherURL string    `json:"publisher_url"`
	CreatedAt    time.Time `json:"created_at"`
}

// ─── Globals ─────────────────────────────────────────────────────────────────

var pool *pgxpool.Pool

// Shared HTTP client that follows redirects – used for URL resolution
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 15 {
			return fmt.Errorf("stopped after 15 redirects")
		}
		// Forward User-Agent on every hop
		req.Header.Set("User-Agent", chromeUA)
		return nil
	},
	Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		ResponseHeaderTimeout: 10 * time.Second,
	},
}

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// Strip HTML tags (used for RSS description / content fallback)
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)
var multiSpaceRe = regexp.MustCompile(`\s{2,}`)

// ─── Entry Point ─────────────────────────────────────────────────────────────

func main() {
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		log.Println("DATABASE_URL is not set, skipping DB connection for now")
	} else {
		go func() {
			var lastErr error
			for i := 0; i < 30; i++ {
				currURL := dbURL
				u, err := url.Parse(currURL)
				if err == nil {
					q := u.Query()
					if i >= 10 && strings.Contains(u.Host, ".render.com") {
						hostParts := strings.Split(u.Host, ".")
						if len(hostParts) > 1 {
							u.Host = hostParts[0]
							q.Set("sslmode", "disable")
						}
					} else if q.Get("sslmode") == "" || q.Get("sslmode") == "disable" {
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
						if err = p.Ping(context.Background()); err == nil {
							pool = p
							log.Printf("Connected to DB via %s", maskPassword(currURL))
							break
						}
						p.Close()
					}
					lastErr = err
				} else {
					lastErr = err
				}

				log.Printf("DB connect attempt %d failed via %s: %v", i+1, maskPassword(currURL), lastErr)
				time.Sleep(10 * time.Second)
			}

			if pool == nil {
				log.Println("Could not connect to DB after 30 retries")
				return
			}

			_, err := pool.Exec(context.Background(), `
				CREATE TABLE IF NOT EXISTS news (
					id           SERIAL PRIMARY KEY,
					title        TEXT NOT NULL,
					content      TEXT,
					image_url    TEXT,
					publisher_url TEXT UNIQUE,
					created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
				);
			`)
			if err != nil {
				log.Printf("Failed to create table: %v", err)
				return
			}
			log.Println("DB schema ready.")
			startScheduledScraper()
		}()
	}

	http.HandleFunc("/api/news", getNews)
	http.HandleFunc("/api/news/", getNewsItem) // handles /api/news/{id}
	http.HandleFunc("/api/health", healthCheck)

	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}
	fmt.Printf("Server starting on port %s…\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

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

// stripHTML removes all HTML tags and normalises whitespace.

func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = multiSpaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
// resolveGoogleURL follows the Google News redirect chain and returns the real
// article URL. Google RSS links look like:
//
//	https://news.google.com/rss/articles/CBMi...
//
// They redirect (usually via 301/302/303) to the publisher page.
// If resolution fails for any reason we fall back to the original link.
func resolveGoogleURL(rawURL string) string {
	if !strings.Contains(rawURL, "news.google.com") {
		return rawURL
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return rawURL
	}
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("resolveGoogleURL: request failed %s: %v", rawURL, err)
		return rawURL
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()
	if finalURL != "" && !strings.Contains(finalURL, "news.google.com") {
		log.Printf("Resolved by redirect: %s → %s", rawURL, finalURL)
		return finalURL
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Printf("resolveGoogleURL: parse failed %s: %v", rawURL, err)
		return rawURL
	}

	if canonical, exists := doc.Find(`link[rel="canonical"]`).Attr("href"); exists {
		canonical = strings.TrimSpace(canonical)
		if canonical != "" && !strings.Contains(canonical, "news.google.com") {
			log.Printf("Resolved by canonical: %s → %s", rawURL, canonical)
			return canonical
		}
	}

	if ogURL, exists := doc.Find(`meta[property="og:url"]`).Attr("content"); exists {
		ogURL = strings.TrimSpace(ogURL)
		if ogURL != "" && !strings.Contains(ogURL, "news.google.com") {
			log.Printf("Resolved by og:url: %s → %s", rawURL, ogURL)
			return ogURL
		}
	}

	doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		href, exists := s.Attr("href")
		if !exists {
			return true
		}

		href = strings.TrimSpace(href)

		if strings.HasPrefix(href, "./articles/") {
			return true
		}

		if strings.HasPrefix(href, "/articles/") {
			return true
		}

		if strings.HasPrefix(href, "http") &&
			!strings.Contains(href, "google.com") &&
			!strings.Contains(href, "gstatic.com") {
			finalURL = href
			return false
		}

		return true
	})

	if finalURL != "" && !strings.Contains(finalURL, "news.google.com") {
		log.Printf("Resolved by page link: %s → %s", rawURL, finalURL)
		return finalURL
	}

	log.Printf("Could not resolve Google News URL, keeping original: %s", rawURL)
	return rawURL
}
// ─── HTTP Handlers ────────────────────────────────────────────────────────────

func healthCheck(w http.ResponseWriter, r *http.Request) {
	dbStatus := "Connected"
	if pool == nil {
		dbStatus = "Connecting or Not Configured"
	} else if err := pool.Ping(context.Background()); err != nil {
		dbStatus = "Disconnected: " + err.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "OK",
		"database": dbStatus,
		"time":     time.Now().Format(time.RFC3339),
	})
}

func getNews(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if pool == nil {
		json.NewEncoder(w).Encode([]News{})
		return
	}

	rows, err := pool.Query(context.Background(),
		`SELECT id, title, COALESCE(content,''), COALESCE(image_url,''), publisher_url, created_at
		   FROM news ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		log.Println("Query error:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	newsList := []News{}
	for rows.Next() {
		var n News
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.ImageURL, &n.PublisherURL, &n.CreatedAt); err != nil {
			log.Println("Scan error:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		newsList = append(newsList, n)
	}
	json.NewEncoder(w).Encode(newsList)
}

// getNewsItem handles GET /api/news/{id}  – returns a single news record.
func getNewsItem(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Extract the numeric ID from the path: /api/news/42
	idStr := strings.TrimPrefix(r.URL.Path, "/api/news/")
	idStr = strings.TrimSuffix(idStr, "/")
	if idStr == "" {
		http.Error(w, `{"error":"missing id"}`, http.StatusBadRequest)
		return
	}

	// Validate: digits only
	for _, ch := range idStr {
		if ch < '0' || ch > '9' {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
	}

	if pool == nil {
		http.Error(w, `{"error":"database not ready"}`, http.StatusServiceUnavailable)
		return
	}

	var n News
	err := pool.QueryRow(context.Background(),
		`SELECT id, title, COALESCE(content,''), COALESCE(image_url,''), publisher_url, created_at
		   FROM news WHERE id = $1`, idStr).
		Scan(&n.ID, &n.Title, &n.Content, &n.ImageURL, &n.PublisherURL, &n.CreatedAt)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(n)
}

// ─── Scheduler ────────────────────────────────────────────────────────────────

func startScheduledScraper() {
	log.Println("Starting background scraper scheduler…")
	go runScraper()

	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			runScraper()
		}
	}()
}

// ─── Scraper ──────────────────────────────────────────────────────────────────

func runScraper() {
	log.Println("Scraping cycle started…")
	fp := gofeed.NewParser()

	// Try Amharic soccer news first, fall back to English
	amharicQuery := url.QueryEscape("እግር ኳስ")
	feedURL := fmt.Sprintf("https://news.google.com/rss/search?q=%s&hl=am&gl=ET&ceid=ET:am", amharicQuery)

	feed, err := fp.ParseURL(feedURL)
	if err != nil || len(feed.Items) == 0 {
		log.Println("Amharic feed failed or empty, falling back to English soccer feed:", err)
		feed, err = fp.ParseURL("https://news.google.com/rss/search?q=soccer&hl=en-US&gl=US&ceid=US:en")
		if err != nil {
			log.Println("English feed also failed:", err)
			return
		}
	}

	log.Printf("Feed returned %d items.", len(feed.Items))

	// Process each item sequentially with a small delay to avoid rate-limits
	for _, item := range feed.Items {
		scrapeAndStore(item)
		time.Sleep(2 * time.Second) // polite crawl delay
	}
	log.Println("Scraping cycle completed.")
}

// scrapeAndStore resolves the real article URL, scrapes its full content and
// image, then persists the record. It falls back to the RSS snippet when the
// live page cannot be scraped.
func scrapeAndStore(item *gofeed.Item) {
	if pool == nil {
		return
	}

	// ── 1. Resolve the real article URL from the Google redirect ──────────
	realURL := resolveGoogleURL(item.Link)

	// ── 2. Deduplication – check both the Google URL and the resolved URL ──
	var existingID int
	err := pool.QueryRow(context.Background(),
		"SELECT id FROM news WHERE publisher_url = $1 OR publisher_url = $2",
		item.Link, realURL).Scan(&existingID)
	if err == nil {
		log.Printf("Skip (already stored): %s", item.Title)
		return
	}

	// ── 3. Scrape content and image from the real article page ────────────
	content, imageURL := scrapeArticlePage(realURL)

	// ── 4. Fallbacks when live scraping returned nothing useful ───────────
	if len(strings.TrimSpace(content)) < 100 {
		// Try gofeed's parsed Content (from <content:encoded> in RSS)
		if item.Content != "" {
			content = stripHTML(item.Content)
		}
	}
	if len(strings.TrimSpace(content)) < 100 {
		// Last resort: use the RSS <description> snippet
		content = stripHTML(item.Description)
	}

	// ── 5. Persist ────────────────────────────────────────────────────────
	_, err = pool.Exec(context.Background(),
		`INSERT INTO news (title, content, image_url, publisher_url, created_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (publisher_url) DO NOTHING`,
		item.Title,
		strings.TrimSpace(content),
		imageURL,
		realURL,
		time.Now(),
	)
	if err != nil {
		log.Printf("DB insert error for '%s': %v", item.Title, err)
	} else {
		preview := content
		if len(preview) > 80 {
			preview = preview[:80] + "…"
		}
		log.Printf("Stored (%d chars): %s | %s", len(content), item.Title, preview)
	}
}

// scrapeArticlePage visits the given URL and extracts the article body text
// and the primary image (og:image / twitter:image / first <img> in article).
//
// Strategy (in priority order):
//  1. Paragraphs inside <article> — most semantic news sites
//  2. Paragraphs inside common article-body class patterns
//  3. All <p> tags as a generic fallback
//
// Colly follows redirects automatically, so this also handles any remaining
// server-side redirects that the HTTP client may not have resolved yet.
func scrapeArticlePage(articleURL string) (content, imageURL string) {
	c := colly.NewCollector(
		colly.UserAgent(chromeUA),
		colly.MaxBodySize(5*1024*1024), // 5 MB cap
	)

	// Timeout so we never hang on a single page
	c.SetRequestTimeout(15 * time.Second)

	// Politely limit request rate
	_ = c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 1,
		Delay:       1 * time.Second,
	})

	var paragraphs []string

	// ── Primary: paragraphs strictly inside <article> ────────────────────
	c.OnHTML("article p", func(e *colly.HTMLElement) {
		t := strings.TrimSpace(e.Text)
		if len(t) > 30 { // ignore very short strings (dates, captions, etc.)
			paragraphs = append(paragraphs, t)
		}
	})

	// ── Secondary: common article-body class patterns ─────────────────────
	articleBodySelectors := []string{
		"[class*='article-body'] p",
		"[class*='article_body'] p",
		"[class*='story-body'] p",
		"[class*='story_body'] p",
		"[class*='post-content'] p",
		"[class*='post_content'] p",
		"[class*='entry-content'] p",
		"[class*='entry_content'] p",
		"[class*='article-content'] p",
		"[class*='articleBody'] p",
		"[class*='article-text'] p",
		"[class*='body-text'] p",
		"[class*='news-body'] p",
		"[class*='main-content'] p",
		"[itemprop='articleBody'] p",
	}
	for _, sel := range articleBodySelectors {
		sel := sel
		c.OnHTML(sel, func(e *colly.HTMLElement) {
			if len(paragraphs) > 0 {
				return // already got content from <article>
			}
			t := strings.TrimSpace(e.Text)
			if len(t) > 30 {
				paragraphs = append(paragraphs, t)
			}
		})
	}

	// ── Tertiary: all <p> tags (generic fallback) ─────────────────────────
	c.OnHTML("p", func(e *colly.HTMLElement) {
		if len(paragraphs) > 0 {
			return // already have content from above
		}
		t := strings.TrimSpace(e.Text)
		if len(t) > 30 {
			paragraphs = append(paragraphs, t)
		}
	})

	// ── Image: og:image → twitter:image → first article <img> ────────────
	c.OnHTML("meta[property='og:image']", func(e *colly.HTMLElement) {
		if imageURL == "" {
			imageURL = strings.TrimSpace(e.Attr("content"))
		}
	})
	c.OnHTML("meta[name='og:image']", func(e *colly.HTMLElement) {
		if imageURL == "" {
			imageURL = strings.TrimSpace(e.Attr("content"))
		}
	})
	c.OnHTML("meta[property='twitter:image']", func(e *colly.HTMLElement) {
		if imageURL == "" {
			imageURL = strings.TrimSpace(e.Attr("content"))
		}
	})
	c.OnHTML("meta[name='twitter:image']", func(e *colly.HTMLElement) {
		if imageURL == "" {
			imageURL = strings.TrimSpace(e.Attr("content"))
		}
	})
	c.OnHTML("article img[src]", func(e *colly.HTMLElement) {
		if imageURL == "" {
			src := strings.TrimSpace(e.Attr("src"))
			// Skip tiny icons / tracking pixels
			if src != "" && !strings.Contains(src, "pixel") && !strings.Contains(src, "icon") {
				imageURL = src
			}
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Colly error on %s (HTTP %d): %v", r.Request.URL, r.StatusCode, err)
	})

	if err := c.Visit(articleURL); err != nil {
		log.Printf("Visit error %s: %v", articleURL, err)
	}

	// Deduplicate paragraphs and join
	seen := map[string]bool{}
	var unique []string
	for _, p := range paragraphs {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}
	content = strings.Join(unique, "\n\n")
	return content, imageURL
}

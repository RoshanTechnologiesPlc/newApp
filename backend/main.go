package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
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

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 15 {
			return fmt.Errorf("stopped after 15 redirects")
		}

		req.Header.Set("User-Agent", chromeUA)
		return nil
	},
	Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

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
					id            SERIAL PRIMARY KEY,
					title         TEXT NOT NULL,
					content       TEXT,
					image_url     TEXT,
					publisher_url TEXT UNIQUE,
					created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
	http.HandleFunc("/api/news/", getNewsItem)
	http.HandleFunc("/api/health", healthCheck)

	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}

	fmt.Printf("Server starting on port %s…\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

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

func stripHTML(s string) string {
	s = html.UnescapeString(s)
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = multiSpaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ─── Google News URL Resolver ────────────────────────────────────────────────

// resolveGoogleURL tries 4 methods in order of reliability to turn a
// news.google.com RSS link into the real publisher URL.
// If all methods fail it returns the original Google URL — the caller
// must NOT skip articles just because resolution failed; we still store
// the article content from the RSS feed.
func resolveGoogleURL(item *gofeed.Item) string {
	rawURL := item.Link

	if !strings.Contains(rawURL, "news.google.com") {
		return rawURL
	}

	// ── Method 1: extract href from RSS <description> HTML ────────────────
	// Google News RSS puts the real article URL inside an <a href="..."> in
	// the description field. This is the most reliable method – no HTTP needed.
	if u := extractURLFromDescription(item.Description); isRealPublisherURL(u) {
		log.Printf("Resolved via RSS description: %s", u)
		return u
	}

	// ── Method 2: check item.Links for any non-Google URL ─────────────────
	for _, link := range item.Links {
		if isRealPublisherURL(link) {
			log.Printf("Resolved via item.Links: %s", link)
			return link
		}
	}

	// ── Method 3: base64-decode the article ID and scan for https:// ──────
	if u := decodeGoogleBase64URL(rawURL); isRealPublisherURL(u) {
		log.Printf("Resolved via base64 decode: %s", u)
		return u
	}

	// ── Method 4: HTTP fetch + body scan (last resort) ────────────────────
	if u := resolveByHTTPFetch(rawURL); isRealPublisherURL(u) {
		log.Printf("Resolved via HTTP fetch: %s", u)
		return u
	}

	log.Printf("Could not resolve Google News URL, will store with RSS content: %s", rawURL)
	return rawURL
}

// extractURLFromDescription parses the HTML in an RSS <description> field
// and returns the first non-Google href it finds.
// Google News descriptions look like:
//
//	<a href="https://bbc.com/sport/...">BBC Sport</a>
func extractURLFromDescription(desc string) string {
	if desc == "" {
		return ""
	}
	// Unescape HTML entities like &amp; → &
	desc = html.UnescapeString(desc)

	hrefRe := regexp.MustCompile(`href=["'](https?://[^"']+)["']`)
	for _, m := range hrefRe.FindAllStringSubmatch(desc, -1) {
		if len(m) > 1 && isRealPublisherURL(m[1]) {
			return m[1]
		}
	}
	return ""
}

func isRealPublisherURL(value string) bool {
	value = strings.TrimSpace(value)

	if value == "" {
		return false
	}

	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return false
	}

	blockedHosts := []string{
		"news.google.com",
		"google.com",
		"gstatic.com",
		"googleusercontent.com",
	}

	for _, blocked := range blockedHosts {
		if strings.Contains(value, blocked) {
			return false
		}
	}

	return true
}

func decodeGoogleBase64URL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	parts := strings.Split(u.Path, "/")
	if len(parts) == 0 {
		return ""
	}

	id := strings.TrimSpace(parts[len(parts)-1])
	if id == "" {
		return ""
	}

	decoded, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(id)
		if err != nil {
			return ""
		}
	}

	text := html.UnescapeString(string(decoded))
	text = strings.ReplaceAll(text, `\/`, `/`)
	text = strings.ReplaceAll(text, `\u003d`, `=`)
	text = strings.ReplaceAll(text, `\u0026`, `&`)

	urlRe := regexp.MustCompile(`https?://[^\x00-\x20"'\\<>]+`)
	matches := urlRe.FindAllString(text, -1)

	for _, candidate := range matches {
		candidate = strings.TrimSpace(candidate)

		if isRealPublisherURL(candidate) {
			return candidate
		}
	}

	return ""
}

func resolveByHTTPFetch(rawURL string) string {
	// Try both the /rss/articles/ and /articles/ variants
	candidates := []string{
		strings.Replace(rawURL, "/rss/articles/", "/articles/", 1),
		rawURL,
	}

	for _, articleURL := range candidates {
		req, err := http.NewRequest("GET", articleURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", chromeUA)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Referer", "https://news.google.com/")

		resp, err := httpClient.Do(req)
		if err != nil {
			log.Printf("resolveByHTTPFetch failed for %s: %v", articleURL, err)
			continue
		}

		// Check if we landed on a real publisher page via HTTP redirect
		if finalURL := resp.Request.URL.String(); isRealPublisherURL(finalURL) {
			resp.Body.Close()
			return finalURL
		}

		// Read body and scan for real URLs embedded in HTML / JS
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 200*1024))
		resp.Body.Close()
		if err != nil {
			continue
		}

		body := html.UnescapeString(string(bodyBytes))
		body = strings.ReplaceAll(body, `\/`, `/`)

		// Priority: href= links first (more reliable than JS values)
		hrefRe := regexp.MustCompile(`href=["'](https?://[^"']+)["']`)
		for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
			if len(m) > 1 && isRealPublisherURL(m[1]) {
				return m[1]
			}
		}

		// Fallback: any https:// URL in the page
		urlRe := regexp.MustCompile(`https?://[^\s"'<>\\]+`)
		for _, candidate := range urlRe.FindAllString(body, -1) {
			if isRealPublisherURL(strings.TrimRight(candidate, ".,;)")) {
				return strings.TrimRight(candidate, ".,;)")
			}
		}
	}

	return ""
}

// ─── HTTP Handlers ───────────────────────────────────────────────────────────

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
		   FROM news
		  ORDER BY created_at DESC
		  LIMIT 50`)
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

func getNewsItem(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	idStr := strings.TrimPrefix(r.URL.Path, "/api/news/")
	idStr = strings.TrimSuffix(idStr, "/")

	if idStr == "" {
		http.Error(w, `{"error":"missing id"}`, http.StatusBadRequest)
		return
	}

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
		   FROM news
		  WHERE id = $1`, idStr).
		Scan(&n.ID, &n.Title, &n.Content, &n.ImageURL, &n.PublisherURL, &n.CreatedAt)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(n)
}

// ─── Scheduler ───────────────────────────────────────────────────────────────

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

// ─── Scraper ─────────────────────────────────────────────────────────────────

func runScraper() {
	log.Println("Scraping cycle started…")

	fp := gofeed.NewParser()

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

	for _, item := range feed.Items {
		scrapeAndStore(item)
		time.Sleep(2 * time.Second)
	}

	log.Println("Scraping cycle completed.")
}

func scrapeAndStore(item *gofeed.Item) {
	if pool == nil {
		return
	}

	// Resolve the real publisher URL using all available methods
	realURL := resolveGoogleURL(item)

	// ── Deduplication ────────────────────────────────────────────────────
	var existingID int
	err := pool.QueryRow(context.Background(),
		"SELECT id FROM news WHERE publisher_url = $1 OR publisher_url = $2",
		realURL, item.Link).Scan(&existingID)
	if err == nil {
		log.Printf("Skip (already stored): %s", item.Title)
		return
	}

	// ── Scrape the article page if we have a real publisher URL ──────────
	var content, imageURL string
	if isRealPublisherURL(realURL) {
		content, imageURL = scrapeArticlePage(realURL)
	}

	// ── Fallbacks for content ────────────────────────────────────────────
	if len(strings.TrimSpace(content)) < 100 && item.Content != "" {
		content = stripHTML(item.Content)
	}
	if len(strings.TrimSpace(content)) < 100 {
		content = stripHTML(item.Description)
	}

	title := stripHTML(item.Title)
	content = strings.TrimSpace(content)

	// Use the best URL we have — real publisher URL if resolved, otherwise
	// the Google News URL. Either way we store the article.
	storeURL := realURL

	_, err = pool.Exec(context.Background(),
		`INSERT INTO news (title, content, image_url, publisher_url, created_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (publisher_url) DO NOTHING`,
		title, content, imageURL, storeURL, time.Now(),
	)
	if err != nil {
		log.Printf("DB insert error for '%s': %v", item.Title, err)
		return
	}

	preview := content
	if len(preview) > 80 {
		preview = preview[:80] + "…"
	}
	log.Printf("Stored (%d chars) [%s]: %s | %s", len(content), storeURL, title, preview)
}

func scrapeArticlePage(articleURL string) (content, imageURL string) {
	c := colly.NewCollector(
		colly.UserAgent(chromeUA),
		colly.MaxBodySize(5*1024*1024),
	)

	c.SetRequestTimeout(15 * time.Second)

	_ = c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 1,
		Delay:       1 * time.Second,
	})

	var paragraphs []string

	c.OnHTML("article p", func(e *colly.HTMLElement) {
		t := strings.TrimSpace(e.Text)
		if len(t) > 30 {
			paragraphs = append(paragraphs, t)
		}
	})

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
		selector := sel

		c.OnHTML(selector, func(e *colly.HTMLElement) {
			if len(paragraphs) > 0 {
				return
			}

			t := strings.TrimSpace(e.Text)
			if len(t) > 30 {
				paragraphs = append(paragraphs, t)
			}
		})
	}

	c.OnHTML("p", func(e *colly.HTMLElement) {
		if len(paragraphs) > 0 {
			return
		}

		t := strings.TrimSpace(e.Text)
		if len(t) > 30 {
			paragraphs = append(paragraphs, t)
		}
	})

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
			if src != "" &&
				!strings.Contains(src, "pixel") &&
				!strings.Contains(src, "icon") {
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

	seen := map[string]bool{}
	var unique []string

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}

	content = strings.Join(unique, "\n\n")

	return content, imageURL
}

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
	"bytes"
"html"
"io"

"github.com/PuerkitoBio/goquery"
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
	s = html.UnescapeString(s)
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

	resolved := decodeGoogleNewsURL(rawURL)
	if resolved != "" && !strings.Contains(resolved, "news.google.com") {
		log.Printf("Resolved Google News URL: %s → %s", rawURL, resolved)
		return resolved
	}

	log.Printf("Could not resolve Google News URL, keeping original: %s", rawURL)
	return rawURL
}

func decodeGoogleNewsURL(rawURL string) string {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("decodeGoogleNewsURL GET failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	body := string(bodyBytes)

	signature := extractGoogleAttr(body, `data-n-a-sg`)
	timestamp := extractGoogleAttr(body, `data-n-a-ts`)

	if signature == "" || timestamp == "" {
		log.Printf("decodeGoogleNewsURL missing signature/timestamp for %s", rawURL)
		return ""
	}

	requestPayload := []any{
		"garturlreq",
		[]any{
			[]any{
				"en-US",
				"US",
				[]any{"FINANCE_TOP_INDICES", "WEB_TEST_1_0_0"},
				nil,
				nil,
				1,
				1,
				"US:en",
				nil,
				180,
				nil,
				nil,
				nil,
				nil,
				nil,
				0,
				nil,
				nil,
				[]any{1608992183, 723341000},
			},
			"en-US",
			"US",
			1,
			[]any{2, 3, 4, 8},
			1,
			0,
			"655000234",
			0,
			0,
			nil,
			0,
		},
		signature,
		timestamp,
	}

	requestPayloadJSON, err := json.Marshal(requestPayload)
	if err != nil {
		return ""
	}

	outerPayload := []any{
		[]any{
			[]any{
				"Fbv4je",
				string(requestPayloadJSON),
				nil,
				"generic",
			},
		},
	}

	outerPayloadJSON, err := json.Marshal(outerPayload)
	if err != nil {
		return ""
	}

	form := url.Values{}
	form.Set("f.req", string(outerPayloadJSON))

	batchReq, err := http.NewRequest(
		"POST",
		"https://news.google.com/_/DotsSplashUi/data/batchexecute?rpcids=Fbv4je",
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		return ""
	}

	batchReq.Header.Set("User-Agent", chromeUA)
	batchReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	batchReq.Header.Set("Accept", "*/*")
	batchReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
	batchReq.Header.Set("Origin", "https://news.google.com")
	batchReq.Header.Set("Referer", rawURL)

	batchResp, err := httpClient.Do(batchReq)
	if err != nil {
		log.Printf("decodeGoogleNewsURL batch request failed: %v", err)
		return ""
	}
	defer batchResp.Body.Close()

	batchBodyBytes, err := io.ReadAll(batchResp.Body)
	if err != nil {
		return ""
	}

	batchBody := string(batchBodyBytes)
	batchBody = strings.ReplaceAll(batchBody, `\/`, `/`)
	batchBody = strings.ReplaceAll(batchBody, `\u003d`, `=`)
	batchBody = strings.ReplaceAll(batchBody, `\u0026`, `&`)

	urlRe := regexp.MustCompile(`https?://[^"\\]+`)
	matches := urlRe.FindAllString(batchBody, -1)

	for _, candidate := range matches {
		if !strings.Contains(candidate, "google.com") &&
			!strings.Contains(candidate, "gstatic.com") {
			return candidate
		}
	}

	return ""
}

func extractGoogleAttr(body string, attr string) string {
	re := regexp.MustCompile(attr + `="([^"]+)"`)
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return html.UnescapeString(match[1])
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

	realURL := resolveGoogleURL(item.Link)

	var existingRealID int
	err := pool.QueryRow(context.Background(),
		"SELECT id FROM news WHERE publisher_url = $1",
		realURL).Scan(&existingRealID)

	if err == nil && !strings.Contains(realURL, "news.google.com") {
		log.Printf("Skip real URL already stored: %s", item.Title)
		return
	}

	var existingGoogleID int
	err = pool.QueryRow(context.Background(),
		"SELECT id FROM news WHERE publisher_url = $1",
		item.Link).Scan(&existingGoogleID)

	hasOldGoogleRow := err == nil

	content, imageURL := scrapeArticlePage(realURL)

	if len(strings.TrimSpace(content)) < 100 {
		if item.Content != "" {
			content = stripHTML(item.Content)
		}
	}

	if len(strings.TrimSpace(content)) < 100 {
		content = stripHTML(item.Description)
	}

	title := stripHTML(item.Title)
	content = strings.TrimSpace(content)

	if hasOldGoogleRow && !strings.Contains(realURL, "news.google.com") {
		_, err = pool.Exec(context.Background(),
			`UPDATE news
			 SET title = $1,
			     content = $2,
			     image_url = $3,
			     publisher_url = $4,
			     created_at = $5
			 WHERE id = $6`,
			title,
			content,
			imageURL,
			realURL,
			time.Now(),
			existingGoogleID,
		)

		if err != nil {
			log.Printf("DB update error for '%s': %v", item.Title, err)
		} else {
			log.Printf("Updated old Google News row to real publisher URL: %s", realURL)
		}

		return
	}

	_, err = pool.Exec(context.Background(),
		`INSERT INTO news (title, content, image_url, publisher_url, created_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (publisher_url) DO NOTHING`,
		title,
		content,
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
		log.Printf("Stored (%d chars): %s | %s", len(content), title, preview)
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

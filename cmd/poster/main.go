package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dghubble/oauth1"
)

type Quote struct {
	ID          int64
	VerseNumber int
	Text        string
	ImagePath   string
}

func main() {
	log.SetFlags(0)

	// --- Config (env) ---
	dbPath := envOr("DHAMMAPADA_DB", "./dhammapada.sqlite")
	dryRun := os.Getenv("DRY_RUN") == "1"

	ck := os.Getenv("X_CONSUMER_KEY")
	cs := os.Getenv("X_CONSUMER_SECRET")
	at := os.Getenv("X_ACCESS_TOKEN")
	as := os.Getenv("X_ACCESS_SECRET")

	// Allow running without creds if DRY_RUN=1
	if !dryRun {
		for k, v := range map[string]string{
			"X_CONSUMER_KEY":    ck,
			"X_CONSUMER_SECRET": cs,
			"X_ACCESS_TOKEN":    at,
			"X_ACCESS_SECRET":   as,
		} {
			if v == "" {
				log.Fatalf("missing required env var: %s", k)
			}
		}
	}

	// --- DB init ---
	db, err := sql.Open("sqlite", dbPath)
	must(err)
	defer db.Close()
	must(db.Ping())

	// --- pick a random unposted quote ---
	q, err := getRandomUnpostedQuote(context.Background(), db)
	must(err)

	// --- format status text (truncate to ~280 incl. hashtags/attribution) ---
	status := formatStatus(q)

	// --- check image file ---
	if err := ensureFile(q.ImagePath); err != nil {
		log.Fatalf("image missing or unreadable: %s (%v)", q.ImagePath, err)
	}

	// --- post (or dry-run preview) ---
	if dryRun {
		fmt.Println("DRY RUN ✅ (no network calls)")
		fmt.Printf("Will post:\n---\n%s\n---\n", status)
		fmt.Printf("Image: %s\n", q.ImagePath)
		os.Exit(0)
	}

	// OAuth1 HTTP client (user context)
	httpClient := newOAuth1HTTPClient(ck, cs, at, as)

	// 1) Upload media (simple upload for ≤5MB still images)
	mediaIDStr, err := uploadMediaSimple(httpClient, q.ImagePath)
	must(err)

	// 2) Post Tweet (v2) with media
	tweetID, err := createTweetV2(httpClient, status, []string{mediaIDStr})
	must(err)

	log.Printf("Posted tweet ID %s", tweetID)

	// Mark as posted
	must(markPosted(context.Background(), db, q.ID))
	log.Printf("Marked verse %d (row id %d) as posted at %s", q.VerseNumber, q.ID, time.Now().Format(time.RFC3339))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func ensureFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _ = f.Read(make([]byte, 1)) // permissions sanity check
	return nil
}

func getRandomUnpostedQuote(ctx context.Context, db *sql.DB) (*Quote, error) {
	const sqlq = `
SELECT id, verse_number, quote, image_path
FROM dhammapada_quotes
WHERE posted_at IS NULL
ORDER BY RANDOM()
LIMIT 1;
`
	row := db.QueryRowContext(ctx, sqlq)
	q := &Quote{}
	if err := row.Scan(&q.ID, &q.VerseNumber, &q.Text, &q.ImagePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no unposted quotes remain")
		}
		return nil, err
	}
	return q, nil
}

func markPosted(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `UPDATE dhammapada_quotes SET posted_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

func formatStatus(q *Quote) string {
	const (
		attribution = "— Dhammapada (F. Max Müller)"
		hashtags    = "#Buddhism #Dhammapada #Buddha"
		maxLen      = 280
	)
	body := strings.TrimSpace(q.Text)
	header := fmt.Sprintf("Verse %d — ", q.VerseNumber)
	tail := " " + attribution + " " + hashtags

	text := header + body + tail
	if len([]rune(text)) <= maxLen {
		return text
	}

	ellipsis := "…"
	reserve := len([]rune(tail)) + len([]rune(ellipsis))
	head := []rune(header)
	bodyRunes := []rune(body)
	avail := maxLen - len(head) - reserve
	if avail < 20 {
		avail = 20
	}
	if avail > len(bodyRunes) {
		avail = len(bodyRunes)
	}
	trunc := string(bodyRunes[:avail])
	return string(head) + trunc + ellipsis + tail
}

// --- OAuth1 HTTP client (user context) ---

func newOAuth1HTTPClient(consumerKey, consumerSecret, accessToken, accessSecret string) *http.Client {
	cfg := oauth1.NewConfig(consumerKey, consumerSecret)
	tok := oauth1.NewToken(accessToken, accessSecret)
	return cfg.Client(context.Background(), tok)
}

// --- Media upload (v1.1 simple upload) ---

type mediaUploadResp struct {
	MediaID          int64  `json:"media_id"`
	MediaIDString    string `json:"media_id_string"`
	ExpiresAfterSecs int    `json:"expires_after_secs"`
}

func uploadMediaSimple(httpClient *http.Client, imagePath string) (string, error) {
	// Simple upload: for images <= 5MB
	// Endpoint: https://upload.twitter.com/1.1/media/upload.json
	f, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Field name must be "media"
	part, err := w.CreateFormFile("media", filepath.Base(imagePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://upload.twitter.com/1.1/media/upload.json", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("media upload failed: status=%d body=%s", resp.StatusCode, string(b))
	}

	var r mediaUploadResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.MediaIDString == "" && r.MediaID != 0 {
		r.MediaIDString = strconv.FormatInt(r.MediaID, 10)
	}
	if r.MediaIDString == "" {
		return "", fmt.Errorf("media upload: missing media_id_string")
	}
	return r.MediaIDString, nil
}

// --- Create Tweet (v2) ---

type createTweetReq struct {
	Text  string            `json:"text"`
	Media *createTweetMedia `json:"media,omitempty"`
}
type createTweetMedia struct {
	MediaIDs []string `json:"media_ids"`
}
type createTweetResp struct {
	Data struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"data"`
}

func createTweetV2(httpClient *http.Client, text string, mediaIDs []string) (string, error) {
	reqBody := createTweetReq{Text: text}
	if len(mediaIDs) > 0 {
		reqBody.Media = &createTweetMedia{MediaIDs: mediaIDs}
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(&reqBody); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.twitter.com/2/tweets", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create tweet failed: status=%d body=%s", resp.StatusCode, string(b))
	}

	var r createTweetResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.Data.ID == "" {
		return "", fmt.Errorf("create tweet: missing id in response")
	}
	return r.Data.ID, nil
}

// --- helpers for reading image MIME ---

func readImage(path string) ([]byte, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	mime := "image/jpeg"
	switch ext {
	case ".png":
		mime = "image/png"
	case ".gif":
		mime = "image/gif"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	}
	return b, mime, nil
}


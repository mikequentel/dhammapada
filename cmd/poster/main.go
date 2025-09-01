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
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dghubble/oauth1"
)

type Text struct {
	ID     int64
	Label  string   // e.g., "151" or "58–59"
	Body   string   // verse text
	Images []string // 0..n filesystem paths (we'll cap to 4 on post)
}

func main() {
	log.SetFlags(0)

	// --- Config (env) ---
	dbPath := envOr("DHAMMAPADA_DB", "./data/dhammapada.sqlite")
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

	// --- pick a random unposted text + images ---
	t, err := getRandomUnpostedTextWithImages(context.Background(), db)
	must(err)

	status := formatStatus(t.Label, t.Body)

	// --- Dry-run preview ---
	if dryRun {
		fmt.Println("DRY RUN ✅ (no network calls)")
		fmt.Printf("Status:\n---\n%s\n---\n", status)
		if len(t.Images) == 0 {
			fmt.Println("Images: (none)")
		} else {
			fmt.Println("Images:")
			for _, p := range t.Images {
				fmt.Println(" -", p)
			}
		}
		os.Exit(0)
	}

	// --- OAuth1 user-context HTTP client ---
	httpClient := newOAuth1HTTPClient(ck, cs, at, as)

	// --- upload up to 4 images ---
	mediaIDs, err := uploadImages(httpClient, t.Images)
	must(err)

	// --- create tweet (v2) with media ---
	tweetID, err := createTweetV2(httpClient, status, mediaIDs)
	must(err)
	log.Printf("Posted tweet ID %s", tweetID)

	// --- mark as posted ---
	_, err = db.ExecContext(context.Background(),
		`UPDATE texts SET posted_at = CURRENT_TIMESTAMP, x_post_id = ? WHERE id = ?`,
		tweetID, t.ID)
	must(err)

	log.Printf("Marked text_id=%d (label=%s) as posted at %s", t.ID, t.Label, time.Now().Format(time.RFC3339))
}

// ===================== DB =====================

func getRandomUnpostedTextWithImages(ctx context.Context, db *sql.DB) (*Text, error) {
	const pick = `
SELECT id, label, text_body
FROM texts
WHERE posted_at IS NULL
ORDER BY RANDOM()
LIMIT 1;
`
	t := &Text{}
	if err := db.QueryRowContext(ctx, pick).Scan(&t.ID, &t.Label, &t.Body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no unposted texts remain")
		}
		return nil, err
	}

	const imgs = `
SELECT path
FROM images
WHERE text_id = ?
ORDER BY ord
LIMIT 4;`
	rows, err := db.QueryContext(ctx, imgs, t.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		// sanity check the file
		if err := ensureFile(p); err != nil {
			return nil, fmt.Errorf("image missing or unreadable: %s (%w)", p, err)
		}
		t.Images = append(t.Images, p)
	}
	return t, rows.Err()
}

// ===================== Status text =====================

func formatStatus(label, body string) string {
	const (
		attribution = "— Dhammapada (F. Max Müller)"
		hashtags    = "#Buddhism #Dhammapada #Buddha"
		maxLen      = 280
	)
	header := fmt.Sprintf("Verse %s — ", label)
	tail := " " + attribution + " " + hashtags
	body = strings.TrimSpace(body)

	text := header + body + tail
	if runeLen(text) <= maxLen {
		return text
	}
	ellipsis := "…"
	avail := maxLen - runeLen(header) - runeLen(tail) - runeLen(ellipsis)
	if avail < 20 {
		avail = 20
	}
	trunc := truncateRunes(body, avail)
	return header + trunc + ellipsis + tail
}

func runeLen(s string) int { return len([]rune(s)) }
func truncateRunes(s string, n int) string {
	rs := []rune(s)
	if n >= len(rs) {
		return s
	}
	return string(rs[:n])
}

// ===================== Files =====================

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
	_, _ = f.Read(make([]byte, 1)) // permission sanity check
	return nil
}

// ===================== X (Twitter) =====================

// OAuth1 user-context HTTP client
func newOAuth1HTTPClient(consumerKey, consumerSecret, accessToken, accessSecret string) *http.Client {
	cfg := oauth1.NewConfig(consumerKey, consumerSecret)
	tok := oauth1.NewToken(accessToken, accessSecret)
	return cfg.Client(context.Background(), tok)
}

// Upload multiple images (simple upload, ≤5MB each). Returns media_id strings.
func uploadImages(httpClient *http.Client, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if len(paths) > 4 {
		paths = paths[:4]
	}
	ids := make([]string, 0, len(paths))
	for _, p := range paths {
		id, err := uploadMediaSimple(httpClient, p)
		if err != nil {
			return nil, fmt.Errorf("upload %s: %w", p, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// --- v1.1 media/upload (simple upload) ---

type mediaUploadResp struct {
	MediaID       int64  `json:"media_id"`
	MediaIDString string `json:"media_id_string"`
}

func uploadMediaSimple(httpClient *http.Client, imagePath string) (string, error) {
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
	if r.MediaIDString != "" {
		return r.MediaIDString, nil
	}
	if r.MediaID != 0 {
		return fmt.Sprintf("%d", r.MediaID), nil
	}
	return "", fmt.Errorf("media upload: missing media_id")
}

// --- v2 create tweet ---

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

// ===================== misc =====================

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


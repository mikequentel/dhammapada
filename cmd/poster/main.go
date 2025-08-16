package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/dghubble/go-twitter/twitter"
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
			"X_CONSUMER_KEY":   ck,
			"X_CONSUMER_SECRET": cs,
			"X_ACCESS_TOKEN":   at,
			"X_ACCESS_SECRET":  as,
		} {
			if v == "" {
				log.Fatalf("missing required env var: %s", k)
			}
		}
	}

	// --- DB init ---
	db, err := sql.Open("sqlite3", dbPath)
	must(err)
	defer db.Close()

	// Ensure DB is reachable
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

	// X/Twitter client
	client := newTwitterClient(ck, cs, at, as)

	// Upload media
	mediaID, err := uploadMedia(client, q.ImagePath)
	must(err)

	// Post tweet with media
	tweet, _, err := client.Statuses.Update(status, &twitter.StatusUpdateParams{
		MediaIds: []int64{mediaID},
	})
	must(err)

	log.Printf("Posted tweet ID %d", tweet.ID)

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
	// Basic read to ensure permissions
	_, _ = f.Read(make([]byte, 1))
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
	// Base text: Verse number + quote
	// Attribution: F. Max Müller (public domain translation)
	// Hashtags are optional; tweak to your taste.
	const (
		attribution = "— Dhammapada (F. Max Müller)"
		hashtags    = "#Buddhism #Dhammapada #Buddha"
		maxLen      = 280
	)

	body := strings.TrimSpace(q.Text)
	header := fmt.Sprintf("Verse %d — ", q.VerseNumber)
	tail := " " + attribution + " " + hashtags

	// Compose and truncate intelligently if needed
	text := header + body + tail
	if len([]rune(text)) <= maxLen {
		return text
	}

	// Leave room for ellipsis + tail
	ellipsis := "…"
	// reserve tail + space
	reserve := len([]rune(tail)) + len([]rune(ellipsis))
	// Ensure header is kept
	head := []rune(header)
	bodyRunes := []rune(body)
	avail := maxLen - len(head) - reserve
	if avail < 20 { // fallback safeguard
		avail = 20
	}
	if avail > len(bodyRunes) {
		avail = len(bodyRunes)
	}
	trunc := string(bodyRunes[:avail])

	return string(head) + trunc + ellipsis + tail
}

func newTwitterClient(consumerKey, consumerSecret, accessToken, accessSecret string) *twitter.Client {
	config := oauth1.NewConfig(consumerKey, consumerSecret)
	token := oauth1.NewToken(accessToken, accessSecret)
	httpClient := config.Client(context.Background(), token)
	return twitter.NewClient(httpClient)
}

func uploadMedia(client *twitter.Client, imagePath string) (int64, error) {
	data, mime, err := readImage(imagePath)
	if err != nil {
		return 0, err
	}
	media, _, err := client.Media.Upload(data, mime)
	if err != nil {
		return 0, err
	}
	return media.MediaID, nil
}

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
	}
	return b, mime, nil
}


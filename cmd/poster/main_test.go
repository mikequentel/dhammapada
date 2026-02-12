package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/mikequentel/dhammapada/internal/model"
)

// ===================== normalizeLabel =====================

func TestNormalizeLabel(t *testing.T) {
	tests := []struct {
		label string
		want  string
	}{
		{"151", "151"},
		{"58, 59", "58-59"},
		{"58,59", "58-59"},
		{"58–59", "58-59"},  // en dash
		{"  42  ", "42"},    // whitespace trimming
		{"1, 2, 3", "1-2-3"},
		{"100–102", "100-102"},
		{"a b", "ab"}, // spaces removed
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got := normalizeLabel(tt.label)
			if got != tt.want {
				t.Errorf("normalizeLabel(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

// ===================== runeLen =====================

func TestRuneLen(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"hello", 5},
		{"café", 4},
		{"日本語", 3},
		{"🙂", 1},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := runeLen(tt.input)
			if got != tt.want {
				t.Errorf("runeLen(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ===================== truncateRunes =====================

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty", "", 5, ""},
		{"no truncation needed", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncate ASCII", "hello world", 5, "hello"},
		{"truncate multibyte", "日本語テスト", 3, "日本語"},
		{"zero length", "hello", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateRunes(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

// ===================== envOr =====================

func TestEnvOr(t *testing.T) {
	const key = "TEST_ENVVAR_DHAMMAPADA_XYZ"

	// Unset: should return default.
	os.Unsetenv(key)
	if got := envOr(key, "default_val"); got != "default_val" {
		t.Errorf("envOr unset = %q, want %q", got, "default_val")
	}

	// Set: should return env value.
	os.Setenv(key, "custom")
	defer os.Unsetenv(key)
	if got := envOr(key, "default_val"); got != "custom" {
		t.Errorf("envOr set = %q, want %q", got, "custom")
	}
}

// ===================== formatStatus =====================

func TestFormatStatus_Short(t *testing.T) {
	status := formatStatus("1", "Short verse.")
	if !strings.HasPrefix(status, "1: Short verse.") {
		t.Errorf("expected status to start with label and body, got: %s", status)
	}
	if !strings.Contains(status, "— Dhammapada (F Max Müller)") {
		t.Errorf("expected attribution in status, got: %s", status)
	}
	if !strings.Contains(status, "#dhammapada") {
		t.Errorf("expected hashtag in status, got: %s", status)
	}
	if runeLen(status) > 280 {
		t.Errorf("status exceeds 280 chars: %d", runeLen(status))
	}
}

func TestFormatStatus_Truncation(t *testing.T) {
	longBody := strings.Repeat("word ", 100)
	status := formatStatus("42", longBody)

	if runeLen(status) > 280 {
		t.Errorf("status exceeds 280 runes: %d", runeLen(status))
	}
	if !strings.Contains(status, "…") {
		t.Errorf("expected ellipsis in truncated status, got: %s", status)
	}
	if !strings.HasPrefix(status, "42: ") {
		t.Errorf("expected status to start with label, got: %s", status)
	}
}

func TestFormatStatus_ExactlyMaxLen(t *testing.T) {
	// Build a body that, combined with the header and tail, is exactly 280 runes.
	header := "1: "
	tail := " — Dhammapada (F Max Müller) #dhammapada #buddha #siddharthagautama"
	avail := 280 - runeLen(header) - runeLen(tail)
	body := strings.Repeat("a", avail)

	status := formatStatus("1", body)
	if runeLen(status) != 280 {
		t.Errorf("expected exactly 280 runes, got %d", runeLen(status))
	}
	if strings.Contains(status, "…") {
		t.Errorf("should not contain ellipsis when body fits exactly")
	}
}

// ===================== existsFile =====================

func TestExistsFile(t *testing.T) {
	// Existing file.
	tmp, err := os.CreateTemp(t.TempDir(), "exist-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	if !existsFile(tmp.Name()) {
		t.Errorf("existsFile(%q) = false, want true", tmp.Name())
	}

	// Non-existing file.
	if existsFile(filepath.Join(t.TempDir(), "no-such-file")) {
		t.Error("existsFile(non-existent) = true, want false")
	}

	// Directory.
	if existsFile(t.TempDir()) {
		t.Error("existsFile(directory) = true, want false")
	}
}

// ===================== ensureFile =====================

func TestEnsureFile(t *testing.T) {
	dir := t.TempDir()

	// Valid file.
	f := filepath.Join(dir, "ok.txt")
	os.WriteFile(f, []byte("hello"), 0644)
	if err := ensureFile(f); err != nil {
		t.Errorf("ensureFile(valid) = %v, want nil", err)
	}

	// Non-existing file.
	if err := ensureFile(filepath.Join(dir, "missing")); err == nil {
		t.Error("ensureFile(missing) = nil, want error")
	}

	// Directory.
	if err := ensureFile(dir); err == nil {
		t.Error("ensureFile(dir) = nil, want error")
	}
}

// ===================== deriveImagePaths =====================

func TestDeriveImagePaths(t *testing.T) {
	// Create a temp images directory with some test files.
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	imgDir := filepath.Join(tmpDir, "images")
	os.Mkdir(imgDir, 0755)

	// Create test image files.
	for _, name := range []string{"42.jpg", "58-59.jpg", "100.png"} {
		os.WriteFile(filepath.Join(imgDir, name), []byte("fake-image"), 0644)
	}

	tests := []struct {
		label     string
		wantCount int
	}{
		{"42", 1},
		{"58, 59", 1},  // comma-space normalized to hyphen
		{"58–59", 1},   // en dash normalized
		{"100", 1},     // png extension
		{"999", 0},     // no matching images
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			paths, err := deriveImagePaths(tt.label)
			if err != nil {
				t.Fatalf("deriveImagePaths(%q) error: %v", tt.label, err)
			}
			if len(paths) != tt.wantCount {
				t.Errorf("deriveImagePaths(%q) returned %d paths, want %d: %v", tt.label, len(paths), tt.wantCount, paths)
			}
		})
	}
}

func TestDeriveImagePaths_MultipleImages(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	imgDir := filepath.Join(tmpDir, "images")
	os.Mkdir(imgDir, 0755)

	// Create numbered variants: 7.jpg, 7-1.jpg, 7-2.jpg, 7-3.jpg, 7-4.jpg
	for _, name := range []string{"7.jpg", "7-1.jpg", "7-2.jpg", "7-3.jpg", "7-4.jpg"} {
		os.WriteFile(filepath.Join(imgDir, name), []byte("fake"), 0644)
	}

	paths, err := deriveImagePaths("7")
	if err != nil {
		t.Fatal(err)
	}
	// Should return at most 4.
	if len(paths) > 4 {
		t.Errorf("expected at most 4 images, got %d", len(paths))
	}
	if len(paths) < 4 {
		t.Errorf("expected 4 images (7.jpg + 7-1..7-3), got %d: %v", len(paths), paths)
	}
}

// ===================== diagnoseHTTPError =====================

func TestDiagnoseHTTPError_V2(t *testing.T) {
	v2Body := `{"title":"Forbidden","detail":"not allowed","type":"https://api.twitter.com/2/problems/forbidden"}`
	resp := &http.Response{
		StatusCode: 403,
		Header:     http.Header{"X-Access-Level": {"read-write"}},
	}
	msg := diagnoseHTTPError(resp, []byte(v2Body), "POST /2/tweets")
	if !strings.Contains(msg, "Forbidden") {
		t.Errorf("expected v2 title in message, got: %s", msg)
	}
	if !strings.Contains(msg, "not allowed") {
		t.Errorf("expected v2 detail in message, got: %s", msg)
	}
	if !strings.Contains(msg, "403") {
		t.Errorf("expected status code in message, got: %s", msg)
	}
}

func TestDiagnoseHTTPError_V1(t *testing.T) {
	v1Body := `{"errors":[{"code":89,"message":"Invalid or expired token."}]}`
	resp := &http.Response{
		StatusCode: 401,
		Header:     http.Header{},
	}
	msg := diagnoseHTTPError(resp, []byte(v1Body), "POST /1.1/media/upload.json")
	if !strings.Contains(msg, "89") {
		t.Errorf("expected v1 error code in message, got: %s", msg)
	}
	if !strings.Contains(msg, "Invalid or expired token") {
		t.Errorf("expected v1 error message in message, got: %s", msg)
	}
}

func TestDiagnoseHTTPError_Fallback(t *testing.T) {
	resp := &http.Response{
		StatusCode: 500,
		Header:     http.Header{},
	}
	msg := diagnoseHTTPError(resp, []byte("something unexpected"), "GET /endpoint")
	if !strings.Contains(msg, "500") {
		t.Errorf("expected status code in fallback, got: %s", msg)
	}
	if !strings.Contains(msg, "something unexpected") {
		t.Errorf("expected raw body in fallback, got: %s", msg)
	}
}

// ===================== getRandomUnpostedTextAndImages =====================

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE texts (
		id        INTEGER PRIMARY KEY,
		label     TEXT NOT NULL UNIQUE,
		text_body TEXT NOT NULL,
		posted_at TEXT NULL,
		x_post_id TEXT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGetRandomUnpostedTextAndImages_NoRows(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	_, err := getRandomUnpostedTextAndImages(context.Background(), db)
	if err == nil {
		t.Fatal("expected error for empty table, got nil")
	}
	if !strings.Contains(err.Error(), "no unposted texts remain") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetRandomUnpostedTextAndImages_AllPosted(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	db.Exec(`INSERT INTO texts (id, label, text_body, posted_at, x_post_id)
		VALUES (1, '1', 'verse one', '2025-01-01', '12345')`)

	_, err := getRandomUnpostedTextAndImages(context.Background(), db)
	if err == nil {
		t.Fatal("expected error when all texts are posted, got nil")
	}
}

func TestGetRandomUnpostedTextAndImages_ReturnsUnposted(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Change to a temp dir so deriveImagePaths won't find anything in images/.
	origDir, _ := os.Getwd()
	os.Chdir(t.TempDir())
	defer os.Chdir(origDir)

	db.Exec(`INSERT INTO texts (id, label, text_body) VALUES (1, '42', 'The wise one')`)
	db.Exec(`INSERT INTO texts (id, label, text_body, posted_at) VALUES (2, '43', 'Already posted', '2025-01-01')`)

	txt, err := getRandomUnpostedTextAndImages(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if txt.ID != 1 || txt.Label != "42" || txt.Body != "The wise one" {
		t.Errorf("unexpected text: %+v", txt)
	}
}

// ===================== createTweetV2 =====================

func TestCreateTweetV2_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json content-type, got %s", ct)
		}

		var req model.TweetReq
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		if req.Text == "" {
			t.Error("expected non-empty text in tweet request")
		}

		w.WriteHeader(200)
		json.NewEncoder(w).Encode(model.TweetResp{
			Data: struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			}{ID: "9876543210", Text: req.Text},
		})
	}))
	defer srv.Close()

	// Monkey-patch: use httptest server by creating a custom HTTP client that
	// rewrites URLs. Since createTweetV2 uses a hardcoded URL, we use a
	// custom transport.
	client := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
	}

	id, err := createTweetV2(client, "Hello world", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != "9876543210" {
		t.Errorf("expected tweet ID 9876543210, got %s", id)
	}
}

func TestCreateTweetV2_WithMedia(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req model.TweetReq
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)

		if req.Media == nil || len(req.Media.MediaIDs) != 2 {
			t.Errorf("expected 2 media IDs, got: %+v", req.Media)
		}

		w.WriteHeader(200)
		json.NewEncoder(w).Encode(model.TweetResp{
			Data: struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			}{ID: "111222333"},
		})
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
	}

	id, err := createTweetV2(client, "Post with images", []string{"media1", "media2"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "111222333" {
		t.Errorf("expected tweet ID 111222333, got %s", id)
	}
}

func TestCreateTweetV2_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"title":"Forbidden","detail":"not allowed"}`))
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
	}

	_, err := createTweetV2(client, "fail", nil)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "Forbidden") {
		t.Errorf("expected Forbidden in error, got: %v", err)
	}
}

// ===================== uploadImages =====================

func TestUploadImages_Empty(t *testing.T) {
	ids, err := uploadImages(http.DefaultClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ids != nil {
		t.Errorf("expected nil for empty paths, got %v", ids)
	}
}

func TestUploadImages_CapsAtFour(t *testing.T) {
	// Create 5 temp image files.
	dir := t.TempDir()
	var paths []string
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".jpg")
		os.WriteFile(p, []byte("fake-image-data"), 0644)
		paths = append(paths, p)
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(model.MediaUploadResp{
			MediaIDString: "media_" + string(rune('0'+callCount)),
		})
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
	}

	ids, err := uploadImages(client, paths)
	if err != nil {
		t.Fatal(err)
	}
	// Should have uploaded exactly 4 (the cap).
	if len(ids) != 4 {
		t.Errorf("expected 4 media IDs, got %d", len(ids))
	}
}

// ===================== uploadMediaSimple =====================

func TestUploadMediaSimple_Success(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.jpg")
	os.WriteFile(imgPath, []byte("fake-image-data"), 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "multipart/form-data") {
			t.Errorf("expected multipart content type, got %s", ct)
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(model.MediaUploadResp{
			MediaIDString: "1234567890",
			MediaID:       1234567890,
		})
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
	}

	id, err := uploadMediaSimple(client, imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if id != "1234567890" {
		t.Errorf("expected media ID 1234567890, got %s", id)
	}
}

func TestUploadMediaSimple_NumericFallback(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.jpg")
	os.WriteFile(imgPath, []byte("fake"), 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		// Return only numeric media_id, no media_id_string.
		json.NewEncoder(w).Encode(model.MediaUploadResp{
			MediaID: 9999999999,
		})
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
	}

	id, err := uploadMediaSimple(client, imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if id != "9999999999" {
		t.Errorf("expected fallback to numeric ID, got %s", id)
	}
}

func TestUploadMediaSimple_MissingMediaID(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.jpg")
	os.WriteFile(imgPath, []byte("fake"), 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
	}

	_, err := uploadMediaSimple(client, imgPath)
	if err == nil {
		t.Fatal("expected error for missing media_id")
	}
	if !strings.Contains(err.Error(), "missing media_id") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ===================== rewriteTransport =====================

// rewriteTransport redirects all HTTP requests to a local httptest server,
// allowing us to test functions that use hardcoded external URLs.
type rewriteTransport struct {
	base   http.RoundTripper
	target string // e.g., "http://127.0.0.1:PORT"
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	// Parse target to get host.
	req.URL.Host = strings.TrimPrefix(rt.target, "http://")
	return rt.base.RoundTrip(req)
}

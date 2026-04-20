package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- Unit tests for helper functions ---

func TestHumanSize(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}
	for _, tt := range tests {
		got := humanSize(tt.input)
		if got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFileIcon(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"photo.jpg", "🖼"},
		{"photo.JPEG", "🖼"},
		{"movie.mp4", "🎬"},
		{"song.mp3", "🎵"},
		{"doc.pdf", "📕"},
		{"readme.md", "📄"},
		{"data.csv", "📊"},
		{"archive.zip", "📦"},
		{"route.gpx", "🗺"},
		{"unknown.xyz", "📎"},
		{"noext", "📎"},
	}
	for _, tt := range tests {
		got := fileIcon(tt.name)
		if got != tt.want {
			t.Errorf("fileIcon(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()

	// First file should be the original name
	got := uniquePath(dir, "test.txt")
	if filepath.Base(got) != "test.txt" {
		t.Errorf("expected test.txt, got %s", filepath.Base(got))
	}

	// Create the file, next call should return test_1.txt
	os.WriteFile(got, []byte("hello"), 0644)
	got2 := uniquePath(dir, "test.txt")
	if filepath.Base(got2) != "test_1.txt" {
		t.Errorf("expected test_1.txt, got %s", filepath.Base(got2))
	}

	// Create that too, should get test_2.txt
	os.WriteFile(got2, []byte("world"), 0644)
	got3 := uniquePath(dir, "test.txt")
	if filepath.Base(got3) != "test_2.txt" {
		t.Errorf("expected test_2.txt, got %s", filepath.Base(got3))
	}
}

func TestUniquePathNoExtension(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte(""), 0644)
	got := uniquePath(dir, "Makefile")
	if filepath.Base(got) != "Makefile_1" {
		t.Errorf("expected Makefile_1, got %s", filepath.Base(got))
	}
}

func TestMakeURL(t *testing.T) {
	cfg := &config{ip: "192.168.1.100", port: 8888}

	got := cfg.makeURL("")
	if got != "http://192.168.1.100:8888" {
		t.Errorf("makeURL() = %q, want http://192.168.1.100:8888", got)
	}

	got = cfg.makeURL("/download/file.txt")
	if got != "http://192.168.1.100:8888/download/file.txt" {
		t.Errorf("makeURL(/download/file.txt) = %q", got)
	}
}

func TestMakeURLEncrypted(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	cfg := &config{ip: "192.168.1.100", port: 9090, encrypt: true, key: key}

	got := cfg.makeURL("")
	if !strings.HasPrefix(got, "http://192.168.1.100:9090#") {
		t.Errorf("encrypted URL should contain fragment, got %q", got)
	}
	if !strings.Contains(got, "#") {
		t.Errorf("encrypted URL missing # fragment")
	}
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("aaa"), 0644)
	os.WriteFile(filepath.Join(dir, "beta.jpg"), []byte("bbbbb"), 0644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	files := listFiles(dir)

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Name != "alpha.txt" {
		t.Errorf("first file should be alpha.txt, got %s", files[0].Name)
	}
	if files[1].Name != "beta.jpg" {
		t.Errorf("second file should be beta.jpg, got %s", files[1].Name)
	}
	if files[1].Icon != "🖼" {
		t.Errorf("beta.jpg icon should be 🖼, got %s", files[1].Icon)
	}
}

func TestListFilesEmpty(t *testing.T) {
	dir := t.TempDir()
	files := listFiles(dir)
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

// --- Encryption tests ---

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("The quick brown fox jumps over the lazy dog")

	encrypted, err := encryptData(key, plaintext)
	if err != nil {
		t.Fatalf("encryptData failed: %v", err)
	}

	if bytes.Equal(encrypted, plaintext) {
		t.Error("encrypted data should differ from plaintext")
	}

	decrypted, err := decryptData(key, encrypted)
	if err != nil {
		t.Fatalf("decryptData failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("decrypted data should match original plaintext")
	}
}

func TestEncryptDecryptLargeData(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	// 1MB of random data
	plaintext := make([]byte, 1<<20)
	rand.Read(plaintext)

	encrypted, err := encryptData(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := decryptData(key, encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("round-trip failed for large data")
	}
}

func TestEncryptDecryptEmpty(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	encrypted, err := encryptData(key, []byte{})
	if err != nil {
		t.Fatalf("encrypt empty failed: %v", err)
	}

	decrypted, err := decryptData(key, encrypted)
	if err != nil {
		t.Fatalf("decrypt empty failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Error("decrypted empty data should be empty")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	encrypted, _ := encryptData(key1, []byte("secret"))
	_, err := decryptData(key2, encrypted)
	if err == nil {
		t.Error("decrypting with wrong key should fail")
	}
}

func TestDecryptTruncated(t *testing.T) {
	_, err := decryptData(make([]byte, 32), []byte{1, 2, 3})
	if err == nil {
		t.Error("decrypting truncated data should fail")
	}
}

func TestEncryptProducesUniqueNonces(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	plaintext := []byte("same input")

	enc1, _ := encryptData(key, plaintext)
	enc2, _ := encryptData(key, plaintext)

	if bytes.Equal(enc1, enc2) {
		t.Error("two encryptions of same plaintext should differ (unique nonces)")
	}
}

// --- HTTP handler tests ---

func newTestConfig(t *testing.T) *config {
	t.Helper()
	return &config{
		ip:        "127.0.0.1",
		port:      0,
		outputDir: t.TempDir(),
	}
}

func createMultipartUpload(t *testing.T, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for name, content := range files {
		part, err := writer.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		part.Write(content)
	}
	writer.Close()
	return body, writer.FormDataContentType()
}

func TestUploadHandler(t *testing.T) {
	cfg := newTestConfig(t)
	handler := makeUploadHandler(cfg)

	body, contentType := createMultipartUpload(t, map[string][]byte{
		"hello.txt": []byte("hello world"),
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	content, err := os.ReadFile(filepath.Join(cfg.outputDir, "hello.txt"))
	if err != nil {
		t.Fatalf("uploaded file not found: %v", err)
	}
	if string(content) != "hello world" {
		t.Errorf("file content = %q, want %q", string(content), "hello world")
	}
}

func TestUploadHandlerMultipleFiles(t *testing.T) {
	cfg := newTestConfig(t)
	handler := makeUploadHandler(cfg)

	body, contentType := createMultipartUpload(t, map[string][]byte{
		"a.txt": []byte("aaa"),
		"b.txt": []byte("bbb"),
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// Both files should exist
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(cfg.outputDir, name)); err != nil {
			t.Errorf("file %s not found: %v", name, err)
		}
	}
}

func TestUploadHandlerDedup(t *testing.T) {
	cfg := newTestConfig(t)
	handler := makeUploadHandler(cfg)

	// Upload same filename twice
	for i := 0; i < 2; i++ {
		body, contentType := createMultipartUpload(t, map[string][]byte{
			"dup.txt": []byte(fmt.Sprintf("version %d", i)),
		})
		req := httptest.NewRequest(http.MethodPost, "/upload", body)
		req.Header.Set("Content-Type", contentType)
		rr := httptest.NewRecorder()
		handler(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("upload %d failed: %d", i, rr.Code)
		}
	}

	// Should have dup.txt and dup_1.txt
	if _, err := os.Stat(filepath.Join(cfg.outputDir, "dup.txt")); err != nil {
		t.Error("dup.txt not found")
	}
	if _, err := os.Stat(filepath.Join(cfg.outputDir, "dup_1.txt")); err != nil {
		t.Error("dup_1.txt not found")
	}
}

func TestUploadHandlerGetMethod(t *testing.T) {
	cfg := newTestConfig(t)
	handler := makeUploadHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/upload", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /upload should return 405, got %d", rr.Code)
	}
}

func TestUploadHandlerEncrypted(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	cfg := &config{
		ip:        "127.0.0.1",
		port:      0,
		encrypt:   true,
		key:       key,
		outputDir: t.TempDir(),
	}
	handler := makeUploadHandler(cfg)

	plaintext := []byte("encrypted content here")
	encrypted, err := encryptData(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	body, contentType := createMultipartUpload(t, map[string][]byte{
		"secret.txt": encrypted,
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Saved file should be decrypted
	content, err := os.ReadFile(filepath.Join(cfg.outputDir, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "encrypted content here" {
		t.Errorf("decrypted file content = %q", string(content))
	}
}

// --- Send mode HTTP tests ---

func TestSendModeDownload(t *testing.T) {
	// Create a temp file to serve
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.gpx")
	testContent := []byte("<gpx>test route</gpx>")
	os.WriteFile(testFile, testContent, 0644)

	cfg := &config{ip: "127.0.0.1", port: 8888, outputDir: dir}
	_ = cfg
	info, _ := os.Stat(testFile)
	fileName := filepath.Base(testFile)
	hostname, _ := os.Hostname()
	tmpl := loadTemplate("templates/index.html")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			errorPage(w, "Not Found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, pageData{
			Hostname:     hostname,
			DownloadFile: fileName,
			DownloadSize: humanSize(info.Size()),
			ShowUpload:   true,
		})
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		reqFile := strings.TrimPrefix(r.URL.Path, "/download/")
		if reqFile != fileName {
			errorPage(w, "Not Found", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, testFile)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test index page
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("GET / = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "test.gpx") {
		t.Error("index page should contain filename")
	}

	// Test download
	resp, err = http.Get(ts.URL + "/download/test.gpx")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(body, testContent) {
		t.Error("downloaded content doesn't match")
	}

	// Test 404 for wrong file
	resp, err = http.Get(ts.URL + "/download/wrong.txt")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("GET /download/wrong.txt = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// Test 404 for unknown path
	resp, err = http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("GET /nonexistent = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- Browse mode HTTP tests ---

func TestBrowseModeListing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("jpeg data"), 0644)
	os.WriteFile(filepath.Join(dir, "doc.pdf"), []byte("pdf data"), 0644)
	os.WriteFile(filepath.Join(dir, ".secret"), []byte("hidden"), 0644)

	cfg := &config{ip: "127.0.0.1", port: 8888, outputDir: dir}
	tmpl := loadTemplate("templates/browse.html")
	hostname, _ := os.Hostname()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			errorPage(w, "Not Found", http.StatusNotFound)
			return
		}
		files := listFiles(dir)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, pageData{
			Hostname:  hostname,
			ShowUpload: true,
			Directory: filepath.Base(dir),
			Files:     files,
		})
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		reqFile := strings.TrimPrefix(r.URL.Path, "/download/")
		if reqFile == "" || strings.Contains(reqFile, "/") || strings.Contains(reqFile, "..") {
			errorPage(w, "Not Found", http.StatusNotFound)
			return
		}
		absFile := filepath.Join(dir, reqFile)
		finfo, err := os.Stat(absFile)
		if err != nil || finfo.IsDir() {
			errorPage(w, "Not Found", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, absFile)
	})
	mux.HandleFunc("/upload", makeUploadHandler(cfg))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test listing page
	resp, _ := http.Get(ts.URL + "/")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(body)

	if !strings.Contains(page, "photo.jpg") {
		t.Error("listing should contain photo.jpg")
	}
	if !strings.Contains(page, "doc.pdf") {
		t.Error("listing should contain doc.pdf")
	}
	if strings.Contains(page, ".secret") {
		t.Error("listing should NOT contain hidden files")
	}

	// Test file download
	resp, _ = http.Get(ts.URL + "/download/photo.jpg")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "jpeg data" {
		t.Errorf("downloaded content = %q", string(body))
	}

	// Test path traversal blocked
	resp, _ = http.Get(ts.URL + "/download/../../../etc/passwd")
	if resp.StatusCode != 404 {
		t.Errorf("path traversal should return 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- Error page tests ---

func TestErrorPageRendersHTML(t *testing.T) {
	rr := httptest.NewRecorder()
	errorPage(rr, "Not Found", http.StatusNotFound)

	if rr.Code != 404 {
		t.Errorf("expected 404, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Not Found") {
		t.Error("error page should contain title")
	}
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("error page should be HTML")
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type should be text/html, got %s", ct)
	}
}

func TestErrorPage500(t *testing.T) {
	rr := httptest.NewRecorder()
	errorPage(rr, "Server Error", http.StatusInternalServerError)
	if rr.Code != 500 {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// --- Cross-language crypto interop test ---

func TestCryptoInteropGoEncryptJSDecrypt(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found, skipping interop test")
	}

	key := make([]byte, 32)
	rand.Read(key)
	plaintext := "Hello from Go! 🌈🔒"

	encrypted, err := encryptData(key, []byte(plaintext))
	if err != nil {
		t.Fatalf("Go encrypt failed: %v", err)
	}

	// Force pure JS path by patching out Web Crypto detection
	jsScript := `
const fs = require('fs');
let code = fs.readFileSync('templates/crypto.js', 'utf8');
code = code.replace('var subtle = isSecure &&', 'var subtle = false &&');
eval(code);
const key = Buffer.from(process.env.KEY, 'hex');
const data = Buffer.from(process.env.DATA, 'hex');
async function main() {
  if (BifrostCrypto.hasWebCrypto) { console.error('ERROR: Web Crypto not disabled!'); process.exit(1); }
  const plain = await BifrostCrypto.decrypt(new Uint8Array(key), new Uint8Array(data));
  process.stdout.write(Buffer.from(plain).toString('utf8'));
}
main().catch(e => { console.error(e.message); process.exit(1); });
`
	cmd := exec.Command("node", "-e", jsScript)
	cmd.Dir = filepath.Join(".")
	cmd.Env = append(os.Environ(),
		"KEY="+hex.EncodeToString(key),
		"DATA="+hex.EncodeToString(encrypted),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("JS decrypt failed: %v\nOutput: %s", err, string(out))
	}

	if string(out) != plaintext {
		t.Errorf("JS decrypted %q, want %q", string(out), plaintext)
	}
}

func TestCryptoInteropJSEncryptGoDecrypt(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found, skipping interop test")
	}

	key := make([]byte, 32)
	rand.Read(key)
	plaintext := "Hello from JS! 🌈🔒"

	// Run JS encryption (forced pure JS)
	jsScript := `
const fs = require('fs');
let code = fs.readFileSync('templates/crypto.js', 'utf8');
code = code.replace('var subtle = isSecure &&', 'var subtle = false &&');
eval(code);
const key = Buffer.from(process.env.KEY, 'hex');
const pt = Buffer.from(process.env.PLAINTEXT, 'utf8');
async function main() {
  if (BifrostCrypto.hasWebCrypto) { console.error('ERROR: Web Crypto not disabled!'); process.exit(1); }
  const enc = await BifrostCrypto.encrypt(new Uint8Array(key), new Uint8Array(pt));
  process.stdout.write(Buffer.from(enc).toString('hex'));
}
main().catch(e => { console.error(e.message); process.exit(1); });
`
	cmd := exec.Command("node", "-e", jsScript)
	cmd.Dir = filepath.Join(".")
	cmd.Env = append(os.Environ(),
		"KEY="+hex.EncodeToString(key),
		"PLAINTEXT="+plaintext,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("JS encrypt failed: %v\nOutput: %s", err, string(out))
	}

	encrypted, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("bad hex from JS: %v", err)
	}

	decrypted, err := decryptData(key, encrypted)
	if err != nil {
		t.Fatalf("Go decrypt failed: %v", err)
	}

	if string(decrypted) != plaintext {
		t.Errorf("Go decrypted %q, want %q", string(decrypted), plaintext)
	}
}

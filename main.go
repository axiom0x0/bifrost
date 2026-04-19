package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed templates/*.html
var templateFS embed.FS

var version = "dev"

const defaultPort = 8888

type config struct {
	ip        string
	port      int
	encrypt   bool
	key       []byte // 32-byte AES-256 key
	outputDir string
}

type pageData struct {
	Hostname     string
	Encrypted    bool
	DownloadFile string
	DownloadSize string
	ShowUpload   bool
	Directory    string
	Files        []fileEntry
}

type fileEntry struct {
	Name string
	Size string
	Icon string
}

func main() {
	showVersion := flag.Bool("v", false, "print version and exit")
	filePath := flag.String("f", "", "file to serve (send mode)")
	port := flag.Int("p", defaultPort, "port to serve on")
	receive := flag.Bool("r", false, "receive-only mode")
	outputDir := flag.String("o", ".", "output directory for received files")
	dirPath := flag.String("d", "", "directory to browse and serve")
	encrypt := flag.Bool("e", false, "enable end-to-end encryption")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "bifrost — bridge files to your phone via QR code\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  bifrost <file> [port]              send a file (+ upload)\n")
		fmt.Fprintf(os.Stderr, "  bifrost -f <file> [-p port]        send a file (+ upload)\n")
		fmt.Fprintf(os.Stderr, "  bifrost -r [-o dir] [-p port]      receive files only\n")
		fmt.Fprintf(os.Stderr, "  bifrost -d <dir> [-p port]         browse a directory (+ upload)\n\n")
		fmt.Fprintf(os.Stderr, "  Add -e to any mode for encrypted transfers.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  bifrost myfile.gpx              send file, allow uploads back\n")
		fmt.Fprintf(os.Stderr, "  bifrost -f photo.jpg -p 9090    send on custom port\n")
		fmt.Fprintf(os.Stderr, "  bifrost -r -o ~/Downloads       receive files only\n")
		fmt.Fprintf(os.Stderr, "  bifrost -d ~/Photos             browse directory\n")
		fmt.Fprintf(os.Stderr, "  bifrost -e myfile.gpx           send with encryption\n")
		fmt.Fprintf(os.Stderr, "  bifrost -e -r                   receive with encryption\n")
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("bifrost %s\n", version)
		os.Exit(0)
	}

	// Handle positional args: bifrost [flags] <file> [port]
	// Go's flag package stops at first non-flag, so we also scan os.Args
	// for any -p/-f/-etc that might appear after positional args
	if *filePath == "" && !*receive && *dirPath == "" {
		// No mode flags set — look for positional file arg
		args := flag.Args()
		if len(args) >= 1 {
			*filePath = args[0]
		}
		if len(args) >= 2 {
			p, err := strconv.Atoi(args[1])
			if err == nil && p >= 1 && p <= 65535 {
				*port = p
			}
		}
	}

	ip := getLocalIP()
	if ip == "" {
		log.Fatal("Could not determine local IP address")
	}

	cfg := &config{
		ip:      ip,
		port:    *port,
		encrypt: *encrypt,
	}

	if *encrypt {
		cfg.key = make([]byte, 32)
		if _, err := rand.Read(cfg.key); err != nil {
			log.Fatalf("Failed to generate encryption key: %v", err)
		}
	}

	setupSignalHandler()

	if *dirPath != "" {
		absDir, err := filepath.Abs(*dirPath)
		if err != nil {
			log.Fatalf("Error resolving directory: %v", err)
		}
		cfg.outputDir = absDir
		runBrowse(cfg, absDir)
	} else if *receive {
		absDir, err := filepath.Abs(*outputDir)
		if err != nil {
			log.Fatalf("Error resolving directory: %v", err)
		}
		cfg.outputDir = absDir
		runReceive(cfg)
	} else {
		if *filePath == "" {
			flag.Usage()
			os.Exit(1)
		}
		cfg.port = *port
		absDir, err := filepath.Abs(*outputDir)
		if err != nil {
			log.Fatalf("Error resolving directory: %v", err)
		}
		cfg.outputDir = absDir
		runSend(cfg, *filePath)
	}
}

// runSend serves a single file for download with an upload form (two-way)
func runSend(cfg *config, filePath string) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Fatalf("Error resolving path: %v", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		log.Fatalf("File not found: %s", absPath)
	}
	if info.IsDir() {
		log.Fatalf("Cannot serve a directory with -f. Use -d instead: %s", absPath)
	}

	fileName := filepath.Base(absPath)
	url := cfg.makeURL("")

	tmpl := loadTemplate("templates/index.html")
	hostname, _ := os.Hostname()

	// If encrypted, encrypt the file once at startup
	var encryptedData []byte
	if cfg.encrypt {
		raw, err := os.ReadFile(absPath)
		if err != nil {
			log.Fatalf("Error reading file: %v", err)
		}
		encryptedData, err = encryptData(cfg.key, raw)
		if err != nil {
			log.Fatalf("Error encrypting file: %v", err)
		}
	}

	printBanner(cfg, "send", url, map[string]string{
		"File": fmt.Sprintf("%s (%s)", fileName, humanSize(info.Size())),
	})

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, pageData{
			Hostname:     hostname,
			Encrypted:    cfg.encrypt,
			DownloadFile: fileName,
			DownloadSize: humanSize(info.Size()),
			ShowUpload:   true,
		})
	})

	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		reqFile := strings.TrimPrefix(r.URL.Path, "/download/")
		if reqFile != fileName {
			http.NotFound(w, r)
			return
		}

		if cfg.encrypt {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
			w.Header().Set("Content-Length", strconv.Itoa(len(encryptedData)))
			w.Write(encryptedData)
		} else {
			contentType := mime.TypeByExtension(filepath.Ext(fileName))
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
			http.ServeFile(w, r, absPath)
		}
		log.Printf("⬇ %s downloaded by %s", fileName, r.RemoteAddr)
	})

	mux.HandleFunc("/upload", makeUploadHandler(cfg))

	serve(cfg, mux)
}

// runReceive accepts uploads only
func runReceive(cfg *config) {
	if err := os.MkdirAll(cfg.outputDir, 0755); err != nil {
		log.Fatalf("Cannot create output directory: %v", err)
	}

	url := cfg.makeURL("")
	tmpl := loadTemplate("templates/index.html")
	hostname, _ := os.Hostname()

	printBanner(cfg, "receive", url, map[string]string{
		"Save": cfg.outputDir,
	})

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, pageData{
			Hostname:   hostname,
			Encrypted:  cfg.encrypt,
			ShowUpload: true,
		})
	})

	mux.HandleFunc("/upload", makeUploadHandler(cfg))

	serve(cfg, mux)
}

// runBrowse serves a directory listing with upload support (two-way)
func runBrowse(cfg *config, dirPath string) {
	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		log.Fatalf("Not a valid directory: %s", dirPath)
	}

	url := cfg.makeURL("")
	tmpl := loadTemplate("templates/browse.html")
	hostname, _ := os.Hostname()

	printBanner(cfg, "browse", url, map[string]string{
		"Dir": dirPath,
	})

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		files := listFiles(dirPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, pageData{
			Hostname:   hostname,
			Encrypted:  cfg.encrypt,
			ShowUpload: true,
			Directory:  filepath.Base(dirPath),
			Files:      files,
		})
	})

	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		reqFile := strings.TrimPrefix(r.URL.Path, "/download/")
		if reqFile == "" || strings.Contains(reqFile, "/") || strings.Contains(reqFile, "..") {
			http.NotFound(w, r)
			return
		}

		absFile := filepath.Join(dirPath, reqFile)
		finfo, err := os.Stat(absFile)
		if err != nil || finfo.IsDir() {
			http.NotFound(w, r)
			return
		}

		if cfg.encrypt {
			raw, err := os.ReadFile(absFile)
			if err != nil {
				http.Error(w, "Read error", http.StatusInternalServerError)
				return
			}
			enc, err := encryptData(cfg.key, raw)
			if err != nil {
				http.Error(w, "Encryption error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", reqFile))
			w.Header().Set("Content-Length", strconv.Itoa(len(enc)))
			w.Write(enc)
		} else {
			contentType := mime.TypeByExtension(filepath.Ext(reqFile))
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", reqFile))
			http.ServeFile(w, r, absFile)
		}
		log.Printf("⬇ %s downloaded by %s", reqFile, r.RemoteAddr)
	})

	mux.HandleFunc("/upload", makeUploadHandler(cfg))

	serve(cfg, mux)
}

func makeUploadHandler(cfg *config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 500MB max
		if err := r.ParseMultipartForm(500 << 20); err != nil {
			http.Error(w, "Error parsing upload: "+err.Error(), http.StatusBadRequest)
			return
		}

		files := r.MultipartForm.File["files"]
		if len(files) == 0 {
			http.Error(w, "No files received", http.StatusBadRequest)
			return
		}

		if err := os.MkdirAll(cfg.outputDir, 0755); err != nil {
			http.Error(w, "Cannot create output directory", http.StatusInternalServerError)
			return
		}

		var saved []string
		for _, fh := range files {
			src, err := fh.Open()
			if err != nil {
				http.Error(w, fmt.Sprintf("Error reading %s: %v", fh.Filename, err), http.StatusInternalServerError)
				return
			}

			data, err := io.ReadAll(src)
			src.Close()
			if err != nil {
				http.Error(w, fmt.Sprintf("Error reading %s: %v", fh.Filename, err), http.StatusInternalServerError)
				return
			}

			// If encrypted, browser sent encrypted data — decrypt it
			if cfg.encrypt {
				data, err = decryptData(cfg.key, data)
				if err != nil {
					http.Error(w, fmt.Sprintf("Decryption failed for %s: %v", fh.Filename, err), http.StatusInternalServerError)
					return
				}
			}

			destPath := uniquePath(cfg.outputDir, fh.Filename)
			if err := os.WriteFile(destPath, data, 0644); err != nil {
				http.Error(w, fmt.Sprintf("Error saving %s: %v", fh.Filename, err), http.StatusInternalServerError)
				return
			}

			savedName := filepath.Base(destPath)
			saved = append(saved, savedName)
			log.Printf("⬆ %s (%s) from %s", savedName, humanSize(int64(len(data))), r.RemoteAddr)
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "✓ Received %d file(s): %s", len(saved), strings.Join(saved, ", "))
	}
}

// Encryption helpers

func encryptData(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 bytes for GCM
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// Prepend nonce to ciphertext
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func decryptData(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// URL helpers

func (cfg *config) makeURL(path string) string {
	base := fmt.Sprintf("http://%s:%d%s", cfg.ip, cfg.port, path)
	if cfg.encrypt {
		keyB64 := base64.RawURLEncoding.EncodeToString(cfg.key)
		return base + "#" + keyB64
	}
	return base
}

// File helpers

func listFiles(dir string) []fileEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{
			Name: e.Name(),
			Size: humanSize(info.Size()),
			Icon: fileIcon(e.Name()),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	return files
}

func fileIcon(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".bmp", ".heic":
		return "🖼"
	case ".mp4", ".mov", ".avi", ".mkv", ".webm":
		return "🎬"
	case ".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a":
		return "🎵"
	case ".pdf":
		return "📕"
	case ".doc", ".docx", ".txt", ".rtf", ".md":
		return "📄"
	case ".xls", ".xlsx", ".csv":
		return "📊"
	case ".zip", ".tar", ".gz", ".rar", ".7z":
		return "📦"
	case ".gpx", ".kml", ".kmz":
		return "🗺"
	default:
		return "📎"
	}
}

func uniquePath(dir, name string) string {
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		dest = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, ext))
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			return dest
		}
	}
}

// Network helpers

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ip := ipnet.IP.String()
			if strings.HasPrefix(ip, "192.168.") {
				return ip
			}
		}
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

// UI helpers

func loadTemplate(name string) *template.Template {
	tmpl, err := template.ParseFS(templateFS, name)
	if err != nil {
		log.Fatalf("Error loading template %s: %v", name, err)
	}
	return tmpl
}

func printQR(url string) {
	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		log.Fatalf("Error generating QR code: %v", err)
	}
	fmt.Println()
	fmt.Println(qr.ToSmallString(false))
}

func printBanner(cfg *config, mode, url string, info map[string]string) {
	printQR(url)
	fmt.Printf("  Mode:  %s", mode)
	if cfg.encrypt {
		fmt.Print(" 🔒")
	}
	fmt.Println()
	for k, v := range info {
		fmt.Printf("  %s:  %s\n", k, v)
	}
	fmt.Printf("  URL:   %s\n", url)
	fmt.Printf("  Port:  %d\n\n", cfg.port)

	switch mode {
	case "send":
		fmt.Println("Scan QR to download (uploads also accepted).")
	case "receive":
		fmt.Println("Scan QR to upload files.")
	case "browse":
		fmt.Println("Scan QR to browse and download files (uploads also accepted).")
	}
	fmt.Println("Press Ctrl+C to stop.\n")
}

func serve(cfg *config, mux *http.ServeMux) {
	addr := fmt.Sprintf(":%d", cfg.port)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nBifrost closed.")
		os.Exit(0)
	}()
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

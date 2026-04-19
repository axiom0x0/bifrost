package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	qrcode "github.com/skip2/go-qrcode"
)

const defaultPort = 8888

const uploadHTML = `<!DOCTYPE html>
<html>
<head>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Bifrost — Upload</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, system-ui, sans-serif; background: #0d1117; color: #e6edf3; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
    .container { text-align: center; padding: 2rem; max-width: 400px; width: 100%%; }
    h1 { font-size: 1.5rem; margin-bottom: 0.5rem; }
    .subtitle { color: #8b949e; margin-bottom: 2rem; }
    .upload-area { border: 2px dashed #30363d; border-radius: 12px; padding: 3rem 1.5rem; margin-bottom: 1rem; cursor: pointer; transition: border-color 0.2s; }
    .upload-area:hover, .upload-area.drag { border-color: #58a6ff; }
    .upload-area p { color: #8b949e; margin-top: 0.5rem; font-size: 0.9rem; }
    input[type="file"] { display: none; }
    .btn { background: #238636; color: white; border: none; padding: 12px 24px; border-radius: 6px; font-size: 1rem; cursor: pointer; width: 100%%; margin-top: 1rem; }
    .btn:disabled { background: #21262d; color: #484f58; cursor: not-allowed; }
    .btn:hover:not(:disabled) { background: #2ea043; }
    .status { margin-top: 1rem; padding: 0.75rem; border-radius: 6px; display: none; }
    .status.success { display: block; background: #0d1f0d; border: 1px solid #238636; color: #3fb950; }
    .status.error { display: block; background: #1f0d0d; border: 1px solid #da3633; color: #f85149; }
    .file-list { text-align: left; margin-top: 1rem; font-size: 0.85rem; color: #8b949e; }
  </style>
</head>
<body>
  <div class="container">
    <h1>⬆ Bifrost</h1>
    <p class="subtitle">Select files to send to %s</p>
    <form id="form" action="/upload" method="POST" enctype="multipart/form-data">
      <div class="upload-area" id="dropzone" onclick="document.getElementById('files').click()">
        📁 Tap to select files
        <p>or drag & drop</p>
      </div>
      <input type="file" id="files" name="files" multiple onchange="showFiles(this.files)">
      <div class="file-list" id="filelist"></div>
      <button type="submit" class="btn" id="submit" disabled>Upload</button>
    </form>
    <div class="status" id="status"></div>
  </div>
  <script>
    const dz = document.getElementById('dropzone');
    const fi = document.getElementById('files');
    dz.addEventListener('dragover', e => { e.preventDefault(); dz.classList.add('drag'); });
    dz.addEventListener('dragleave', () => dz.classList.remove('drag'));
    dz.addEventListener('drop', e => { e.preventDefault(); dz.classList.remove('drag'); fi.files = e.dataTransfer.files; showFiles(fi.files); });
    function showFiles(files) {
      const list = document.getElementById('filelist');
      list.innerHTML = Array.from(files).map(f => '• ' + f.name + ' (' + humanSize(f.size) + ')').join('<br>');
      document.getElementById('submit').disabled = files.length === 0;
    }
    function humanSize(b) {
      if (b < 1024) return b + ' B';
      const u = ['KB','MB','GB'];
      let i = -1;
      do { b /= 1024; i++; } while (b >= 1024 && i < u.length - 1);
      return b.toFixed(1) + ' ' + u[i];
    }
    document.getElementById('form').addEventListener('submit', async e => {
      e.preventDefault();
      const btn = document.getElementById('submit');
      const status = document.getElementById('status');
      btn.disabled = true;
      btn.textContent = 'Uploading...';
      try {
        const resp = await fetch('/upload', { method: 'POST', body: new FormData(e.target) });
        const text = await resp.text();
        if (resp.ok) { status.className = 'status success'; status.textContent = text; }
        else { status.className = 'status error'; status.textContent = text; }
      } catch(err) { status.className = 'status error'; status.textContent = 'Upload failed: ' + err; }
      btn.textContent = 'Upload';
      btn.disabled = false;
    });
  </script>
</body>
</html>`

func main() {
	filePath := flag.String("f", "", "file to serve (send mode)")
	port := flag.Int("p", defaultPort, "port to serve on")
	receive := flag.Bool("r", false, "receive mode — accept uploads from phone")
	outputDir := flag.String("o", ".", "output directory for received files")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "bifrost — bridge files to your phone via QR code\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  bifrost <file> [port]          send a file\n")
		fmt.Fprintf(os.Stderr, "  bifrost -f <file> [-p port]    send a file\n")
		fmt.Fprintf(os.Stderr, "  bifrost -r [-o dir] [-p port]  receive files\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  bifrost myfile.gpx\n")
		fmt.Fprintf(os.Stderr, "  bifrost -f photo.jpg -p 9090\n")
		fmt.Fprintf(os.Stderr, "  bifrost -r\n")
		fmt.Fprintf(os.Stderr, "  bifrost -r -o ~/Downloads\n")
	}

	flag.Parse()

	ip := getLocalIP()
	if ip == "" {
		log.Fatal("Could not determine local IP address")
	}

	setupSignalHandler()

	if *receive {
		runReceive(ip, *port, *outputDir)
	} else {
		// Resolve file path: flag takes priority, then positional arg
		if *filePath == "" {
			if flag.NArg() >= 1 {
				*filePath = flag.Arg(0)
			} else {
				flag.Usage()
				os.Exit(1)
			}
		}
		if flag.NArg() >= 2 {
			p, err := strconv.Atoi(flag.Arg(1))
			if err != nil || p < 1 || p > 65535 {
				log.Fatalf("Invalid port: %s", flag.Arg(1))
			}
			*port = p
		}
		runSend(ip, *port, *filePath)
	}
}

func runSend(ip string, port int, filePath string) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Fatalf("Error resolving path: %v", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		log.Fatalf("File not found: %s", absPath)
	}
	if info.IsDir() {
		log.Fatalf("Cannot serve a directory: %s", absPath)
	}

	fileName := filepath.Base(absPath)
	url := fmt.Sprintf("http://%s:%d/%s", ip, port, fileName)

	printQR(url)
	fmt.Printf("  Mode:  send\n")
	fmt.Printf("  File:  %s (%s)\n", fileName, humanSize(info.Size()))
	fmt.Printf("  URL:   %s\n", url)
	fmt.Printf("  Port:  %d\n\n", port)
	fmt.Println("Scan the QR code with your phone to download.")
	fmt.Println("Press Ctrl+C to stop.\n")

	mux := http.NewServeMux()
	mux.HandleFunc("/"+fileName, func(w http.ResponseWriter, r *http.Request) {
		contentType := mime.TypeByExtension(filepath.Ext(fileName))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))

		log.Printf("⬇ %s downloaded by %s", fileName, r.RemoteAddr)
		http.ServeFile(w, r, absPath)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/"+fileName {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/"+fileName, http.StatusFound)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func runReceive(ip string, port int, outputDir string) {
	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		log.Fatalf("Error resolving output directory: %v", err)
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		log.Fatalf("Cannot create output directory: %v", err)
	}

	hostname, _ := os.Hostname()
	url := fmt.Sprintf("http://%s:%d", ip, port)

	printQR(url)
	fmt.Printf("  Mode:  receive\n")
	fmt.Printf("  Save:  %s\n", absDir)
	fmt.Printf("  URL:   %s\n", url)
	fmt.Printf("  Port:  %d\n\n", port)
	fmt.Println("Scan the QR code with your phone to upload files.")
	fmt.Println("Press Ctrl+C to stop.\n")

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, uploadHTML, hostname)
	})

	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 100MB max
		r.ParseMultipartForm(100 << 20)

		files := r.MultipartForm.File["files"]
		if len(files) == 0 {
			http.Error(w, "No files received", http.StatusBadRequest)
			return
		}

		var saved []string
		for _, fh := range files {
			src, err := fh.Open()
			if err != nil {
				http.Error(w, fmt.Sprintf("Error reading %s: %v", fh.Filename, err), http.StatusInternalServerError)
				return
			}
			defer src.Close()

			destPath := uniquePath(absDir, fh.Filename)
			dst, err := os.Create(destPath)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error saving %s: %v", fh.Filename, err), http.StatusInternalServerError)
				return
			}
			defer dst.Close()

			written, err := io.Copy(dst, src)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error writing %s: %v", fh.Filename, err), http.StatusInternalServerError)
				return
			}

			savedName := filepath.Base(destPath)
			saved = append(saved, savedName)
			log.Printf("⬆ %s (%s) from %s", savedName, humanSize(written), r.RemoteAddr)
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "✓ Received %d file(s): %s", len(saved), strings.Join(saved, ", "))
	})

	addr := fmt.Sprintf(":%d", port)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// uniquePath avoids overwriting existing files by appending a counter
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

func printQR(url string) {
	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		log.Fatalf("Error generating QR code: %v", err)
	}
	fmt.Println()
	fmt.Println(qr.ToSmallString(false))
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

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ip := ipnet.IP.String()
			// Prefer 192.168.x.x addresses
			if strings.HasPrefix(ip, "192.168.") {
				return ip
			}
		}
	}
	// Fallback to any non-loopback IPv4
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
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

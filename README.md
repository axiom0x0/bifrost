# Bifrost

Bridge files between your computer and phone via QR code. No cloud, no cables, no apps — just scan and go.

## Features

- **Two-way transfers** — send and receive in the same session
- **Directory browsing** — serve a folder as a mobile-friendly file picker
- **End-to-end encryption** — AES-256-GCM with key in the QR code, never sent over the wire
- **Zero config** — single binary, no setup, no accounts

## Install

```bash
go install github.com/axiom0x0/bifrost@latest
```

Or build from source:

```bash
git clone https://github.com/axiom0x0/bifrost.git
cd bifrost
go build -o bifrost .
```

## Usage

### Send a file (two-way)

```bash
bifrost myfile.gpx
bifrost -f photo.jpg -p 9090
```

Opens a page with a download button AND an upload form. Your phone can both grab the file and send files back.

### Receive files only

```bash
bifrost -r
bifrost -r -o ~/Downloads
```

Opens a mobile-friendly upload page. Scan the QR code, pick files, they land on your computer.

### Browse a directory

```bash
bifrost -d ~/Photos
bifrost -d /path/to/files -p 9090
```

Serves a file listing with icons and sizes. Tap any file to download. Upload form included.

### Encrypted transfers

Add `-e` to any mode:

```bash
bifrost -e myfile.gpx           # encrypted send
bifrost -e -r                   # encrypted receive
bifrost -e -d ~/Photos          # encrypted browse
```

Generates a random AES-256-GCM key and embeds it in the QR code's URL fragment (the `#` part). The key never travels over the network — it lives only in the QR code and your browser's memory. Files are encrypted/decrypted client-side using the Web Crypto API.

### Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-f` | File to serve (send mode) | — |
| `-r` | Receive-only mode | `false` |
| `-d` | Directory to browse and serve | — |
| `-o` | Output directory for received files | `.` |
| `-p` | Port to serve on | `8888` |
| `-e` | Enable end-to-end encryption | `false` |

## How it works

1. Detects your local network IP
2. Starts an HTTP server with the appropriate mode
3. Generates a QR code pointing to the URL (with encryption key in fragment if `-e`)
4. Your phone scans the QR code and interacts directly over the LAN

No internet required — works entirely on your local network.

## License

MIT

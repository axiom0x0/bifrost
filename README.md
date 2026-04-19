# Bifrost

Bridge files between your computer and phone via QR code. No cloud, no cables, no apps — just scan and go.

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

### Send a file to your phone

```bash
bifrost myfile.gpx
bifrost -f photo.jpg -p 9090
```

Serves the file on your local network and displays a QR code. Scan it with your phone's camera to download.

### Receive files from your phone

```bash
bifrost -r
bifrost -r -o ~/Downloads
bifrost -r -o ~/Downloads -p 9090
```

Opens a mobile-friendly upload page. Scan the QR code, select files on your phone, and they land on your computer.

### Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-f` | File to serve (send mode) | — |
| `-r` | Enable receive mode | `false` |
| `-o` | Output directory for received files | `.` |
| `-p` | Port to serve on | `8888` |

## How it works

1. Detects your local network IP
2. Starts an HTTP server serving the file (send) or an upload page (receive)
3. Generates a QR code pointing to the URL
4. Your phone scans the QR code and downloads/uploads directly over the local network

No internet required — works entirely on your LAN.

## License

MIT

# LiveShare

A CLI tool for temporary file sharing via a WebSocket relay server. Files are streamed directly from the sharer's machine — nothing is uploaded or stored permanently.

## How it works

1. The **host** runs a relay server
2. The **sharer** connects via WebSocket and registers a file (or files)
3. Anyone with the download URL can fetch the file — the server proxies the request back to the sharer over WebSocket

## Quick start

```bash
# Build
make liveshare

# Start the server
liveshare host

# Generate a token
liveshare create mytoken

# Share a file (on another machine or terminal)
liveshare share --server host.example.com/ws/TOKEN file.txt
```

The `share` command prints a download URL that can be opened in any browser or fetched with `curl`.

## Usage

### `liveshare host`

Starts the relay server.

```
--config       Config file path (default: liveshare.json)
--hostname     Public hostname
--listen       Listen address (default: localhost)
--port         Listen port (default: 8080)
--token-file   Path to token file (default: tokens.txt)
--cf-token     Cloudflare tunnel token
--tunnel       Start a cloudflared quick tunnel
```

### `liveshare share [files...]`

Shares files via an existing token.

```
--server       Server URL with token (required, e.g., host/ws/TOKEN)
--name         Display name for the download
-1, --once     One-time share: disconnect after first download
--no-cache     Disable server-side caching
--tar          Archive as tar
--tgz          Archive as gzipped tar
--timeout      Auto-disconnect after duration (e.g., 30m, 1h)
--qr           Display QR code for the download URL
```

**Single file:**
```bash
liveshare share --server host/ws/TOKEN photo.jpg
```

**Directory (auto-zipped):**
```bash
liveshare share --server host/ws/TOKEN ./my-folder
```

**Multiple files (auto-zipped):**
```bash
liveshare share --server host/ws/TOKEN file1.txt file2.txt
```

**Tar/tgz archive:**
```bash
liveshare share --server host/ws/TOKEN --tar ./my-folder
liveshare share --server host/ws/TOKEN --tgz file1.txt file2.txt
```

**One-time share with QR code and 30-minute timeout:**
```bash
liveshare share --server host/ws/TOKEN --once --qr --timeout 30m secret.pdf
```

### `liveshare create [name]`

Generates a random token and appends it to the token file.

## Configuration

- `liveshare.json` — Server config (hostname, listen address, port, cloudflare token, token file path)
- `tokens.txt` — One token per line, optional tab-separated name (`token\tname`)

## Building

```bash
make liveshare        # Build for current platform
make liveshare.exe    # Cross-compile for Windows amd64
go build .            # Quick dev build
```

Requires Go 1.25+.

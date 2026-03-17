# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

LiveShare is a Go CLI tool for temporary file sharing via a WebSocket relay server. A client connects to the server, registers a file, and HTTP clients can download it through the server which proxies the request back to the client over WebSocket.

## Build Commands

```bash
make liveshare        # Build for current platform (trimpath, stripped)
make liveshare.exe    # Cross-compile for Windows amd64
go build .            # Quick dev build
go vet ./...          # Static analysis
```

No test suite exists yet. No linter is configured.

## Architecture

**Three operational modes** via Cobra CLI subcommands:

- **`host`** — Starts the relay server. Loads config from `liveshare.json`, validates tokens from `tokens.txt`, serves WebSocket endpoint (`/ws/{token}`) and HTTP download endpoint (`/d/{id}/{filename}`). Optionally launches a Cloudflare tunnel.
- **`share`** — Client mode. Connects to the server via WebSocket, registers files, then streams file data on demand when download requests arrive. Supports single files, multi-file/directory archive (zip/tar/tgz), one-time shares, no-cache mode, timeout, and QR code display.
- **`create`** — Generates a random token and appends it to `tokens.txt`.

**File download flow:**
1. Client connects via WebSocket → sends `register` → receives `registered` with ShareID
2. HTTP downloader hits `GET /d/{shareID}/{filename}`
3. Server sends `file_request` to client via WebSocket
4. Client streams binary file data back through the WebSocket (or generates zip/tar/tgz archive on the fly for multi-file shares)
5. Server relays data to HTTP response (with flush for progressive download)

**Concurrency note:** Downloads are serialized per share — the single WebSocket connection can only serve one transfer at a time. Subsequent downloaders queue in a buffered channel (size 16) and wait. Fully-cached files are served directly from memory without touching the WebSocket, so those do serve concurrently.

**Disconnection handling:** When an HTTP downloader disconnects mid-transfer, the server detects the cancelled context, stops writing, and drains remaining WebSocket messages until `file_end`/`error` to resync the connection for the next request.

**Key packages:**
- `cmd/` — Cobra command definitions (host, share, create)
- `server/` — HTTP server, WebSocket handler, in-memory store (`Store` maps tokens/shareIDs to `ShareItem`), download handler with 1MB caching for non-one-time shares
- `client/` — WebSocket client that streams local files on request; supports zip/tar/tgz archive streaming via `io.Pipe`
- `protocol/` — Message type constants and JSON message structs
- `config/` — JSON config loading/saving with defaults
- `tunnel/` — Cloudflare tunnel process wrapper (parses tunnel URL from stderr)

## Configuration

- `liveshare.json` — Server config (hostname, listen addr, port, cloudflare token, token file path)
- `tokens.txt` — One token per line, optional tab-separated name (`token\tname`)

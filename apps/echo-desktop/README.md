# Voice Relay Desktop

See the [main README](../../README.md) for usage and installation.

## Building from Source

Requires Go 1.21+ and Node.js 20+ (for PWA build).

```bash
# From the repo root
cd apps/echo-desktop
./build.sh
```

Or manually:

```bash
# 1. Build the PWA
cd packages/pwa && npm install && npm run build && cd ../..

# 2. Copy PWA dist into Go embed directory
cp -r packages/pwa/dist/* apps/echo-desktop/internal/coordinator/pwa_dist/

# 3. Build Go binary
cd apps/echo-desktop
go mod tidy
CGO_ENABLED=1 go build -o VoiceRelay
```

## Project Structure

```
apps/echo-desktop/
  main.go                          # Entry point
  internal/
    config/                        # Config loading/saving
    client/                        # WebSocket echo client
    coordinator/                   # HTTP/WS server, PWA embed
    stt/                           # whisper.cpp STT engine
    llm/                           # llama.cpp text cleanup
    keyboard/                      # Platform-specific key simulation
    tray/                          # Systray menu and status
    setup/                         # First-run wizard + Tailscale detection
    updater/                       # Auto-update from GitHub releases
    icons/                         # Embedded systray icons
```

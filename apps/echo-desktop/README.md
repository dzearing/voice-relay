# Voice Relay Desktop

Unified desktop app for Voice Relay — a single Go binary that handles speech-to-text relay, coordinator services, and system tray integration.

## Features

- **System tray** with connection status icon (cyan=connected, gray=disconnected)
- **Echo client**: receives text from the coordinator and pastes it instantly
- **Coordinator mode** (opt-in): runs the HTTP/WS server, serves the PWA, and handles STT
- **First-run setup wizard**: native dialogs guide you through configuration
- **Tailscale detection**: auto-discovers your Tailscale IP for easy device-to-device setup
- **Auto-update**: checks GitHub releases for new versions on startup

## Installation

### Download Pre-built Binary

Download the latest release for your platform from the [Releases](../../releases) page.

### Build from Source

Requires Go 1.21+, Node.js 20+ (for PWA build), and CGO.

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

## Configuration

On first run, the setup wizard will guide you through configuration. The config file is stored at:
- **Mac**: `~/Library/Application Support/VoiceRelay/config.yaml`
- **Windows**: `%APPDATA%\VoiceRelay\config.yaml`
- **Linux**: `~/.config/voice-relay/config.yaml`

```yaml
name: my-laptop                    # Unique name for this device
coordinator_url: ws://100.x.x.x:53937/ws  # Coordinator's WebSocket URL
output_mode: paste                 # "paste" (instant Ctrl/Cmd+V)

# Coordinator mode (opt-in)
run_as_coordinator: false          # Enable to run the full coordinator
port: 53937                        # HTTP/WS server port
whisper_model: base                # Whisper model: tiny, base, small
llm_model: qwen3-0.6b             # LLM model for text cleanup
llm_enabled: true                  # Enable LLM text cleanup
```

Click "Open Config..." in the tray menu to edit settings, then "Reconnect" to apply.

## Permissions

### macOS
The app needs **Accessibility** permissions to simulate keyboard input:
1. Open System Settings → Privacy & Security → Accessibility
2. Add Voice Relay to the allowed apps

### Windows
May need to run as Administrator for some applications.

## Architecture

```
VoiceRelay (single Go binary)
├── Echo Client (WebSocket → clipboard → paste)
├── Systray UI (connection status, menu)
├── Coordinator (opt-in HTTP/WS server)
│   ├── Embedded PWA (web UI for phone)
│   ├── STT (whisper.cpp CLI / API)
│   └── LLM cleanup (Ollama API)
├── Setup Wizard (native dialogs)
└── Auto-updater (GitHub releases)
```

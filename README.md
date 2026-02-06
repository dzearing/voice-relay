# Voice Relay

Dictate on your phone, type on your computer. A local speech-to-text system that transcribes audio and types it wherever your cursor is.

## Quick Start

### 1. Download Voice Relay

| Platform | Download |
|----------|----------|
| **macOS (M1-M4)** | [VoiceRelay-macOS-arm64.zip](https://github.com/dzearing/voice-relay/releases/latest/download/VoiceRelay-macOS-arm64.zip) |
| **Windows** | [VoiceRelay.exe](https://github.com/dzearing/voice-relay/releases/latest/download/VoiceRelay.exe) |

#### macOS
1. Download and unzip
2. Move `VoiceRelay.app` to Applications
3. Open it (right-click → Open if blocked by Gatekeeper)
4. Grant Accessibility permissions (System Settings → Privacy & Security → Accessibility)

#### Windows
1. Download `VoiceRelay.exe`
2. Run it (click "More info" → "Run anyway" if SmartScreen blocks it)
3. The app appears in your system tray

### 2. First-Run Setup

On first launch, a setup wizard walks you through configuration:

- **Coordinator mode**: Choose whether this machine runs the coordinator (handles STT + serves the web UI)
- **Device name**: Identifies this machine to the coordinator
- **Coordinator URL**: If not running as coordinator, enter the coordinator's address

[Tailscale](https://tailscale.com) is recommended for easy device-to-device networking. The wizard will detect it if installed.

### 3. Use It

1. Open the web UI on your phone at `http://<coordinator-ip>:53937`
2. Select your target device
3. Tap the microphone button to record
4. Tap send — text appears wherever your cursor is on the target device

## Architecture

Voice Relay is a single Go binary. No Node.js, Python, or Ollama required.

```
VoiceRelay (single Go binary)
├── Echo Client    — WebSocket → clipboard → paste
├── Systray UI     — connection status, menu
├── Coordinator    — opt-in HTTP/WS server
│   ├── PWA        — embedded web UI for phone
│   ├── STT        — whisper.cpp (auto-downloaded)
│   └── LLM        — llama.cpp + Qwen3-4B (auto-downloaded)
├── Setup Wizard   — native dialogs
└── Auto-updater   — GitHub releases
```

**Flow:** PWA records audio → Coordinator → whisper.cpp transcribes → Qwen3 cleans up → Echo client pastes

## Configuration

Config is stored at:
- **macOS**: `~/Library/Application Support/VoiceRelay/config.yaml`
- **Windows**: `%APPDATA%\VoiceRelay\config.yaml`
- **Linux**: `~/.config/voice-relay/config.yaml`

```yaml
name: my-laptop                    # Device name
coordinator_url: ws://100.x.x.x:53937/ws  # Coordinator address
output_mode: paste                 # "paste" (instant Ctrl/Cmd+V)

# Coordinator mode (opt-in)
run_as_coordinator: false
port: 53937
whisper_model: base                # tiny, base, or small
llm_model: qwen3-4b               # Text cleanup model
llm_enabled: true
```

Click **"Open Config..."** in the tray menu to edit, then **"Reconnect"** to apply.

## Building from Source

Requires Go 1.21+ and Node.js 20+ (for PWA build).

```bash
cd apps/echo-desktop
./build.sh
```

## Permissions

### macOS
Grant **Accessibility** permissions: System Settings → Privacy & Security → Accessibility

### Windows
May need to run as Administrator for some applications.

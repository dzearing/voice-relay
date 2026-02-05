# Voice Relay Echo Desktop

Native system tray/menu bar app for receiving Voice Relay transcriptions.

## Features

- Lives in your system tray (Windows) or menu bar (Mac)
- Shows connection status with colored icon
- Receives text from Voice Relay and pastes it instantly
- Simple YAML configuration

## Installation

### Download Pre-built Binary

Download the latest release for your platform from the [Releases](../../releases) page.

### Build from Source

Requires Go 1.21+ and CGO (for robotgo).

```bash
# Install dependencies
go mod tidy

# Build for current platform
go build -o voice-relay-echo

# On Mac, you may need to grant Accessibility permissions
```

## Configuration

On first run, a config file is created at:
- **Mac**: `~/Library/Application Support/VoiceRelayEcho/config.yaml`
- **Windows**: `%APPDATA%\VoiceRelayEcho\config.yaml`

```yaml
name: my-laptop           # Unique name for this device
coordinator_url: ws://100.x.x.x:53937/ws  # Your coordinator's Tailscale IP
output_mode: paste        # "paste" (instant) or "type" (character by character)
```

Click "Open Config..." in the menu to edit settings, then "Reconnect" to apply.

## Permissions

### macOS
The app needs **Accessibility** permissions to simulate keyboard input:
1. Open System Settings → Privacy & Security → Accessibility
2. Add Voice Relay Echo to the allowed apps

### Windows
May need to run as Administrator for some applications.

## Building for Release

### macOS (Universal Binary)
```bash
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -o voice-relay-echo-amd64
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o voice-relay-echo-arm64
lipo -create -output voice-relay-echo voice-relay-echo-amd64 voice-relay-echo-arm64
```

### Windows
```bash
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -o voice-relay-echo.exe
```

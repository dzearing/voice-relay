# Voice Relay

Dictate on your phone, type on your computer. A local speech-to-text system that transcribes audio and types it wherever your cursor is.

## Quick Start (Users)

### 1. Download Echo Client

Install the Echo client on any computer where you want text to appear:

| Platform | Download |
|----------|----------|
| **macOS** | [VoiceRelayEcho-macOS.zip](https://github.com/dzearing/voice-relay/releases/latest/download/VoiceRelayEcho-macOS.zip) |
| **Windows** | [VoiceRelayEcho.exe](https://github.com/dzearing/voice-relay/releases/latest/download/VoiceRelayEcho.exe) |

#### macOS Setup
1. Download and unzip `VoiceRelayEcho-macOS.zip`
2. Move `VoiceRelayEcho.app` to Applications
3. Open it (right-click → Open if blocked by Gatekeeper)
4. Grant Accessibility permissions when prompted (System Settings → Privacy & Security → Accessibility)
5. Click the menu bar icon to configure

#### Windows Setup
1. Download `VoiceRelayEcho.exe`
2. Run it (click "More info" → "Run anyway" if SmartScreen blocks it)
3. The app appears in your system tray
4. Right-click the tray icon to configure

### 2. Configure Echo Client

On first run, a config file is created. Click **"Open Config..."** in the menu to edit:

```yaml
name: my-laptop                              # Unique name for this device
coordinator_url: ws://100.x.x.x:53937/ws     # Your coordinator's Tailscale IP
output_mode: paste                           # "paste" (instant) or "type" (slow)
```

After editing, click **"Reconnect"** to apply changes.

### 3. Access the PWA

Open the Voice Relay web app on your phone:

```
https://your-tailscale-hostname:53937
```

Or use Tailscale Funnel for a public HTTPS URL. Add to your home screen for the best experience.

### 4. Use It

1. Select your target device in the PWA
2. Tap the microphone button to record
3. Tap the send button when done
4. Text appears wherever your cursor is on the target device

---

## Server Setup (Self-Hosting)

To run your own Voice Relay server, you'll need a machine running the coordinator, STT service, and Ollama.

### Prerequisites

- Node.js 18+
- Python 3.10+
- [Ollama](https://ollama.ai) with a model (recommended: `qwen3:0.6b`)
- [Tailscale](https://tailscale.com) for secure networking

### Installation

```bash
# Clone the repo
git clone https://github.com/dzearing/voice-relay.git
cd voice-relay

# Install Node.js dependencies
npm install

# Setup Python STT service
cd packages/stt
python -m venv venv
venv\Scripts\activate      # Windows
# source venv/bin/activate # Mac/Linux
pip install -r requirements.txt
cd ../..

# Pull Ollama model
ollama pull qwen3:0.6b
```

### Running Services

**Terminal 1 - STT Service:**
```bash
cd packages/stt
venv\Scripts\activate
uvicorn main:app --host 0.0.0.0 --port 51741
```

**Terminal 2 - Coordinator:**
```bash
npm run dev:coordinator
```

**Terminal 3 - Echo Service (optional, for the server machine):**
```bash
npm run dev:echo
```

### Enable HTTPS (Required for PWA)

Use Tailscale Funnel for valid HTTPS:
```bash
tailscale funnel 53937
```

---

## Architecture

```
┌─────────┐    audio     ┌─────────────┐    audio    ┌─────────┐
│   PWA   │ ──────────► │ Coordinator │ ──────────► │   STT   │
│ (phone) │              │   (home)    │ ◄────────── │ Service │
└─────────┘              └─────────────┘    text     └─────────┘
                               │
                               │ text (cleanup via LLM)
                               ▼
                         ┌─────────┐
                         │ Ollama  │
                         └─────────┘
                               │
                               │ cleaned text
                               ▼
                    ┌─────────────────────┐
                    │    Echo Clients     │
                    │ (Mac/Windows/etc.)  │
                    └─────────────────────┘
```

## Components

| Component | Tech | Description |
|-----------|------|-------------|
| **Coordinator** | Node.js/Express | Routes audio → STT → Ollama → Echo clients |
| **STT Service** | Python/faster-whisper | Local speech-to-text |
| **Echo Client** | Go | System tray app that types/pastes text |
| **PWA** | TypeScript/Vite | Mobile recording interface |

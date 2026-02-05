# Voice Relay

A local speech-to-text system that transcribes audio from your phone and types it on your target machine.

## Components

- **Coordinator** (Node.js) - Orchestrates STT, text cleanup, and routing
- **STT Service** (Python/faster-whisper) - Converts audio to text
- **Echo Service** (Node.js) - Types text on target machine
- **PWA** (TypeScript) - Phone app for recording audio

## Architecture

```
┌─────────┐    audio     ┌─────────────┐    audio    ┌─────────┐
│   PWA   │ ──────────► │ Coordinator │ ──────────► │   STT   │
│ (phone) │              │   (home)    │ ◄────────── │ Service │
└─────────┘              └─────────────┘    text     └─────────┘
                               │
                               │ text (cleanup prompt)
                               ▼
                         ┌─────────┐
                         │ Ollama  │
                         │  LLM    │
                         └─────────┘
                               │
                               │ cleaned text
                               ▼
                         ┌─────────┐
                         │  Echo   │ → Types keystrokes
                         │ Service │ → Copies to clipboard
                         └─────────┘
```

## Prerequisites

- Node.js 18+
- Python 3.10+
- Ollama installed with a model (e.g., llama3.2 or mistral)
- Tailscale configured on all machines

## Setup

### 1. Install Node.js dependencies

```bash
npm install
```

### 2. Setup STT Service

```bash
cd packages/stt
python -m venv venv
venv\Scripts\activate  # Windows
# source venv/bin/activate  # Linux/Mac
pip install -r requirements.txt
```

### 3. Configure Echo Service

Edit `packages/echo/config.json`:
```json
{
  "name": "My-PC",
  "coordinatorUrl": "ws://100.x.x.x:53937/ws"
}
```

## Running

### Start STT Service (port 51741)
```bash
cd packages/stt
venv\Scripts\activate
uvicorn main:app --host 0.0.0.0 --port 51741
```

### Start Coordinator (port 53937)
```bash
npm run dev:coordinator
```

### Start Echo Service (on target machine)
```bash
npm run dev:echo
```

### Access PWA
Open `http://<coordinator-ip>:53937` on your phone and install as PWA.

## Network Configuration

All services communicate over Tailscale. Configure each service with the appropriate Tailscale IP addresses.

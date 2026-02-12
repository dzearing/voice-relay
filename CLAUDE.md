# Voice Relay - Development Notes

## Build & Restart Workflow

After editing Go source code in `apps/echo-desktop/`, always rebuild and restart the local service:

```bash
cd apps/echo-desktop && go build -o VoiceRelay.exe . && ./VoiceRelay.exe --force
```

The `--force` flag kills any existing VoiceRelay instances before starting the new one.

## PWA Dev Server

To develop the PWA frontend with hot-reload, start two things:

1. **Go backend** (serves API + WebSocket on port 53937):

```bash
cd apps/echo-desktop && go build -o VoiceRelay.exe . && ./VoiceRelay.exe --force
```

2. **Vite dev server** (serves PWA on port 5001, proxies API calls to Go backend):

```bash
npm run dev:pwa
```

Then open `http://localhost:5001` in the browser. Edits to `packages/pwa/` will hot-reload.

## Notifications

VoiceRelay has a file-based notification pipeline with TTS audio. Drop a JSON file (with `title` and `summary` fields) into the `pending/` directory and it gets processed, narrated, and pushed to the PWA. See [docs/notifications.md](docs/notifications.md) for the full API and examples.

## Releasing

When releasing a new version, follow [docs/release.md](docs/release.md).

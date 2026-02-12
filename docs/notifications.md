# Notifications

VoiceRelay includes a file-based notification pipeline that accepts JSON files, generates TTS audio, and pushes notifications to the PWA as iOS-style banners with audio playback.

## Architecture

Notifications flow through four directories under `{data-dir}/notifications/`:

```
pending/      →  processing/  →  processed/  →  archived/
(submit here)    (transient)     (live list)     (dismissed)
```

| Platform | `{data-dir}` |
|----------|-------------|
| Windows  | `%APPDATA%\VoiceRelay` |
| macOS    | `~/Library/Application Support/VoiceRelay` |
| Linux    | `~/.config/voice-relay` |

The watcher polls `pending/` every 2 seconds. For each file it:

1. Moves to `processing/`
2. Validates required fields (`title`, `summary`)
3. Generates TTS audio for `summary` and `details`
4. Writes the enriched file to `processed/`
5. Broadcasts a `notifications_updated` WebSocket event to all PWA clients

On startup, any files left in `processing/` are recovered back to `pending/`.

## Creating a Notification

Drop a JSON file into the `pending/` directory. The filename must end in `.json`.

### Required fields

| Field     | Type   | Description |
|-----------|--------|-------------|
| `title`   | string | Short headline shown in the banner |
| `summary` | string | Body text, also used for TTS audio |

### Optional fields

| Field        | Type   | Description |
|--------------|--------|-------------|
| `id`         | string | Unique identifier (defaults to filename stem) |
| `details`    | string | Longer text, shown on expand; also gets TTS audio |
| `priority`   | string | Arbitrary priority label |
| `source`     | string | Where the notification came from (e.g. `"claude-code"`) |
| `created_at` | string | RFC 3339 timestamp (e.g. `2025-02-08T12:00:00Z`) |

### Minimal example

```json
{
  "title": "Build complete",
  "summary": "All 42 tests passed."
}
```

### Full example

```json
{
  "id": "deploy-1707400000000",
  "title": "Deployment finished",
  "summary": "v2.3.1 deployed to production successfully.",
  "details": "All health checks passed. 3 new endpoints active. Rollback window is 30 minutes.",
  "priority": "high",
  "source": "ci-pipeline",
  "created_at": "2025-02-08T20:00:00Z"
}
```

### From a script

```bash
# Bash (macOS/Linux)
cat > ~/Library/Application\ Support/VoiceRelay/notifications/pending/my-notif.json <<'EOF'
{"title": "Task done", "summary": "Finished processing 500 records."}
EOF
```

```powershell
# PowerShell (Windows)
'{"title": "Task done", "summary": "Finished processing 500 records."}' |
  Set-Content "$env:APPDATA\VoiceRelay\notifications\pending\my-notif.json"
```

## HTTP API

All endpoints are served by the Go backend on port 53937.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET`  | `/notifications` | List all processed notifications (newest first) |
| `POST` | `/notifications/dismiss` | Dismiss one notification (`{"id": "..."}`) |
| `POST` | `/notifications/dismiss-all` | Dismiss all notifications |
| `POST` | `/notifications/test` | Generate a random test notification via LLM |

### Response format

`GET /notifications` returns an array of notification objects with all original fields plus:

| Field           | Type   | Description |
|-----------------|--------|-------------|
| `processed_at`  | string | RFC 3339 timestamp when TTS was generated |
| `summary_audio` | string | Base64-encoded WAV audio of the summary |
| `details_audio` | string | Base64-encoded WAV audio of the details (if present) |

## Claude Code Integration

A Claude Code `Stop` hook can automatically create a notification whenever Claude finishes a response. This is configured in `.claude/settings.local.json` and powered by `.claude/hooks/notify-done.ps1`.

The hook reads the session transcript, extracts the user's request and Claude's response, strips markdown, and drops a notification JSON into `pending/`. The result is a TTS-narrated summary pushed to the PWA every time Claude completes work.

See `.claude/hooks/notify-done.ps1` for the implementation.

## Source code

- Notification watcher: `apps/echo-desktop/internal/notifications/notifications.go`
- HTTP handlers: `apps/echo-desktop/internal/coordinator/server.go`
- PWA display: `packages/pwa/src/main.ts`

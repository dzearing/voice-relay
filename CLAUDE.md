# Voice Relay - Development Notes

## Build & Restart Workflow

After editing Go source code in `apps/echo-desktop/`, always rebuild and restart the local service:

```bash
cd apps/echo-desktop && go build -o VoiceRelay.exe . && ./VoiceRelay.exe --force
```

The `--force` flag kills any existing VoiceRelay instances before starting the new one.

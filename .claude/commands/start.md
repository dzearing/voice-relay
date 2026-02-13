Start the VoiceRelay dev inner loop: build and run the Go backend, then start the Vite dev server for the PWA.

1. Build and start the Go backend in the background:
   ```bash
   cd apps/echo-desktop && go build -o VoiceRelay.exe . && ./VoiceRelay.exe --force
   ```
   Run this in the background so it keeps serving on port 53937.

2. Start the Vite dev server in the background:
   ```bash
   npm run dev:pwa
   ```
   This serves the PWA on port 5001 with hot-reload, proxying API calls to the Go backend.

Start both processes in the background and report their status. If either fails to build or start, report the error.

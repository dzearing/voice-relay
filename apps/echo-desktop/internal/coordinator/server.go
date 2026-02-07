package coordinator

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	qrcode "github.com/skip2/go-qrcode"
)

var (
	reg           *registry
	sttFunc       func(audioData []byte, filename string) (string, error)
	llmFunc       func(rawText string) (string, error)
	funcMu        sync.RWMutex
	coordinatorPort int
	externalURL   string // e.g. "http://100.x.x.x:53937" for Tailscale
)

var (
	shortURL      string
	connectionCode string // just the unique part, e.g. "abc123"
)

// SetExternalURL sets the externally-reachable URL for the coordinator (e.g. Tailscale Funnel).
func SetExternalURL(url string) {
	externalURL = url
}

// SetShortURL sets the shortened URL and extracts the connection code.
func SetShortURL(url string) {
	shortURL = url
	// Extract just the code from "https://is.gd/abc123"
	if idx := strings.LastIndex(url, "/"); idx >= 0 {
		connectionCode = url[idx+1:]
	}
}

// GetExternalURL returns the external URL if set.
func GetExternalURL() string {
	return externalURL
}

// GetShortURL returns the short URL if set.
func GetShortURL() string {
	return shortURL
}

// GetConnectionCode returns the short connection code.
func GetConnectionCode() string {
	return connectionCode
}

// SetSTTFunc sets the speech-to-text function used by the /transcribe endpoint.
func SetSTTFunc(fn func(audioData []byte, filename string) (string, error)) {
	funcMu.Lock()
	defer funcMu.Unlock()
	sttFunc = fn
}

// SetLLMFunc sets the text cleanup function used by the /transcribe endpoint.
func SetLLMFunc(fn func(rawText string) (string, error)) {
	funcMu.Lock()
	defer funcMu.Unlock()
	llmFunc = fn
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Start launches the coordinator HTTP/WS server on the given port.
func Start(port int) error {
	reg = newRegistry()
	coordinatorPort = port

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/machines", handleMachines)
	mux.HandleFunc("/transcribe", handleTranscribe)
	mux.HandleFunc("/send-text", handleSendText)
	mux.HandleFunc("/ws", handleWebSocket)
	mux.HandleFunc("/connect", handleConnect)
	mux.HandleFunc("/connect-info", handleConnectInfo)

	// Serve PWA static files (fallback for all other routes)
	mux.Handle("/", pwaHandler())

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Coordinator running on port %d", port)
	log.Printf("PWA available at http://localhost:%d", port)
	log.Printf("WebSocket endpoint: ws://localhost:%d/ws", port)

	return http.ListenAndServe(addr, corsMiddleware(mux))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func handleMachines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, reg.list())
}

func handleSendText(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Target string `json:"target"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if body.Target == "" || body.Text == "" {
		writeJSONError(w, "Missing target or text", http.StatusBadRequest)
		return
	}

	if !reg.sendText(body.Target, body.Text) {
		writeJSONError(w, fmt.Sprintf("Target machine '%s' not connected", body.Target), http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]interface{}{
		"success": true,
		"text":    body.Text,
		"target":  body.Target,
	})
}

func handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form (32MB max)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	target := r.FormValue("target")
	if target == "" {
		writeJSONError(w, "No target machine specified", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		writeJSONError(w, "No audio file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	audioData, err := io.ReadAll(file)
	if err != nil {
		writeJSONError(w, "Failed to read audio file", http.StatusInternalServerError)
		return
	}

	log.Printf("Received audio: %s, size: %d, target: %s", header.Filename, len(audioData), target)

	// 1. Transcribe audio
	funcMu.RLock()
	transcribeFn := sttFunc
	cleanupFn := llmFunc
	funcMu.RUnlock()

	if transcribeFn == nil {
		writeJSONError(w, "STT engine not available", http.StatusServiceUnavailable)
		return
	}

	sttStart := time.Now()
	rawText, err := transcribeFn(audioData, header.Filename)
	sttMs := time.Since(sttStart).Milliseconds()
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Transcription error: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Raw transcription (%dms): %s", sttMs, rawText)

	// If whisper detected no speech, return early without sending
	if isBlankTranscription(rawText) {
		log.Printf("No speech detected, not sending")
		writeJSON(w, map[string]interface{}{
			"success":     false,
			"rawText":     rawText,
			"cleanedText": "",
			"noSpeech":    true,
			"sttMs":       sttMs,
			"llmMs":       int64(0),
		})
		return
	}

	// 2. Clean up text with LLM
	cleanedText := rawText
	var llmMs int64
	if cleanupFn != nil {
		llmStart := time.Now()
		cleaned, err := cleanupFn(rawText)
		llmMs = time.Since(llmStart).Milliseconds()
		if err != nil {
			log.Printf("LLM cleanup failed (%dms), using raw text: %v", llmMs, err)
		} else {
			cleanedText = cleaned
		}
	}
	log.Printf("Cleaned text (%dms): %s", llmMs, cleanedText)

	// 3. Send to target
	if !reg.sendText(target, cleanedText) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":       fmt.Sprintf("Target machine '%s' not connected", target),
			"rawText":     rawText,
			"cleanedText": cleanedText,
			"sttMs":       sttMs,
			"llmMs":       llmMs,
		})
		return
	}

	writeJSON(w, map[string]interface{}{
		"success":     true,
		"rawText":     rawText,
		"cleanedText": cleanedText,
		"target":      target,
		"sttMs":       sttMs,
		"llmMs":       llmMs,
	})
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	log.Println("New WebSocket connection")

	go func() {
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				reg.unregister(conn)
				return
			}

			var msg struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("Invalid WebSocket message: %v", err)
				continue
			}

			if msg.Type == "register" && msg.Name != "" {
				reg.register(msg.Name, conn)
				resp, _ := json.Marshal(map[string]string{
					"type": "registered",
					"name": msg.Name,
				})
				conn.WriteMessage(websocket.TextMessage, resp)
			}
		}
	}()
}

func isBlankTranscription(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	if t == "" {
		return true
	}
	blanks := []string{
		"[blank_audio]", "(blank_audio)", "[blank audio]", "(blank audio)",
		"[no speech]", "(no speech)", "[silence]", "(silence)",
		"you", "thank you.", "thanks for watching!",
	}
	for _, b := range blanks {
		if t == b {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// connectInfo is returned by /connect-info for client auto-configuration.
type connectInfo struct {
	WebSocketURL string `json:"wsUrl"`
	ExternalURL  string `json:"externalUrl"`
	ShortURL     string `json:"shortUrl,omitempty"`
}

func handleConnectInfo(w http.ResponseWriter, r *http.Request) {
	wsURL := fmt.Sprintf("wss://%s/ws", strings.TrimPrefix(strings.TrimPrefix(externalURL, "https://"), "http://"))
	if externalURL == "" {
		wsURL = fmt.Sprintf("ws://localhost:%d/ws", coordinatorPort)
	}
	writeJSON(w, connectInfo{
		WebSocketURL: wsURL,
		ExternalURL:  externalURL,
		ShortURL:     shortURL,
	})
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	url := externalURL
	if url == "" {
		url = fmt.Sprintf("http://localhost:%d", coordinatorPort)
	}

	png, err := qrcode.Encode(url, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}
	b64 := base64.StdEncoding.EncodeToString(png)

	// Show connection code prominently if available
	codeSection := ""
	if connectionCode != "" {
		codeSection = fmt.Sprintf(`<div class="code-section">
      <p class="label">Connection code for other devices:</p>
      <div class="code">%s</div>
    </div>`, connectionCode)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Connect to Voice Relay</title>
<style>
  body { font-family: -apple-system, system-ui, sans-serif; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; background: #0a0a14; color: #e0e0e0; }
  .card { text-align: center; padding: 2rem; max-width: 400px; }
  h1 { font-size: 1.5rem; margin-bottom: 0.5rem; }
  .subtitle { color: #888; margin-bottom: 1.5rem; }
  img { border-radius: 12px; }
  .url { font-family: monospace; font-size: 0.85rem; color: #666; margin-top: 1rem; word-break: break-all; }
  .code-section { margin-top: 2rem; padding-top: 1.5rem; border-top: 1px solid rgba(255,255,255,0.1); }
  .label { color: #888; font-size: 0.9rem; margin-bottom: 0.75rem; }
  .code { font-family: monospace; font-size: 2rem; color: #00d4ff; font-weight: 700; letter-spacing: 0.15em; padding: 14px 28px; background: rgba(0,212,255,0.1); border: 1px solid rgba(0,212,255,0.2); border-radius: 12px; }
  .setup-hint { margin-top: 2rem; padding: 1rem; background: rgba(255,255,255,0.05); border-radius: 10px; color: #888; font-size: 0.85rem; line-height: 1.5; }
  .setup-hint strong { color: #ccc; }
</style></head>
<body><div class="card">
  <h1>Voice Relay</h1>
  <p class="subtitle">Scan to open the web app on your phone</p>
  <img src="data:image/png;base64,%s" width="280" height="280" alt="QR Code">
  <div class="url">%s</div>
  %s
  <div class="setup-hint">
    <strong>Connecting another computer?</strong><br>
    Install Voice Relay, then enter the connection code when prompted during setup.
  </div>
</div></body></html>`, b64, url, codeSection)
}

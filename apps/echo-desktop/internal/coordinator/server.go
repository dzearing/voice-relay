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

	"github.com/voice-relay/echo-desktop/internal/hooks"
	"github.com/voice-relay/echo-desktop/internal/notifications"
)

var (
	reg             *registry
	sttFunc         func(audioData []byte, filename string) (string, error)
	llmFunc         func(rawText string) (string, string, error)
	ttsFunc         func(text, voice, language string) ([]byte, error)
	ttsChangeFunc   func(voiceName string) error              // callback to switch voice at runtime
	ttsPreviewFunc  func(text, voiceName string) ([]byte, error) // preview any voice without changing selection
	agentFunc       func(rawText string, onProgress func(string, string)) (string, error) // talk mode agent function
	funcMu          sync.RWMutex
	coordinatorPort int
	externalURL     string // e.g. "http://100.x.x.x:53937" for Tailscale
	ttsVoice        string // configured TTS voice name
	devURL          string // dev-mode Vite URL (HTTPS via Tailscale)

	notifWatcher *notifications.Watcher
	notifGenFunc func() (map[string]string, error) // generates random notification via LLM
	notifDir     string                             // notification directory for hook install

	interimCache   map[string]string // phrase → base64 WAV
	interimCacheMu sync.RWMutex

	// pendingQuestions tracks AskUserQuestion prompts from Claude Code hooks,
	// keyed by question ID.
	pendingQuestions   map[string]*PendingQuestion
	pendingQuestionsMu sync.RWMutex
)

// PendingQuestion represents an AskUserQuestion intercepted by a PreToolUse hook.
type PendingQuestion struct {
	ID          string              `json:"id"`
	ReplyTarget string              `json:"reply_target"`
	Questions   []QuestionItem      `json:"questions"`
	CreatedAt   string              `json:"created_at"`
	Answered    bool                `json:"answered"`
}

// QuestionItem mirrors Claude Code's AskUserQuestion schema.
type QuestionItem struct {
	Question    string         `json:"question"`
	Header      string         `json:"header"`
	Options     []QuestionOpt  `json:"options"`
	MultiSelect bool           `json:"multiSelect"`
}

// QuestionOpt is a single option in an AskUserQuestion.
type QuestionOpt struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

func init() {
	pendingQuestions = make(map[string]*PendingQuestion)
}

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

// SetDevURL sets the dev-mode Vite URL (HTTPS via Tailscale Funnel).
func SetDevURL(url string) {
	devURL = url
}

// GetDevURL returns the dev-mode URL if set.
func GetDevURL() string {
	return devURL
}

// SetSTTFunc sets the speech-to-text function used by the /transcribe endpoint.
func SetSTTFunc(fn func(audioData []byte, filename string) (string, error)) {
	funcMu.Lock()
	defer funcMu.Unlock()
	sttFunc = fn
}

// SetLLMFunc sets the text cleanup function used by the /transcribe endpoint.
// The function returns (cleaned text, summary, error).
func SetLLMFunc(fn func(rawText string) (string, string, error)) {
	funcMu.Lock()
	defer funcMu.Unlock()
	llmFunc = fn
}

// SetTTSFunc sets the text-to-speech function for audio feedback.
func SetTTSFunc(fn func(text, voice, language string) ([]byte, error)) {
	funcMu.Lock()
	defer funcMu.Unlock()
	ttsFunc = fn
}

// SetTTSVoice sets the configured TTS voice name.
func SetTTSVoice(voice string) {
	ttsVoice = voice
}

// SetTTSChangeFunc sets the callback to switch TTS voice at runtime.
func SetTTSChangeFunc(fn func(voiceName string) error) {
	funcMu.Lock()
	defer funcMu.Unlock()
	ttsChangeFunc = fn
}

// SetTTSPreviewFunc sets the callback to preview any voice without changing selection.
func SetTTSPreviewFunc(fn func(text, voiceName string) ([]byte, error)) {
	funcMu.Lock()
	defer funcMu.Unlock()
	ttsPreviewFunc = fn
}

// SetAgentFunc sets the talk-mode agent function.
func SetAgentFunc(fn func(rawText string, onProgress func(string, string)) (string, error)) {
	funcMu.Lock()
	defer funcMu.Unlock()
	agentFunc = fn
}

// SetNotificationWatcher sets the notification watcher used by the /notifications endpoints.
func SetNotificationWatcher(w *notifications.Watcher) {
	notifWatcher = w
}

// SetNotifGenFunc sets the function used to generate test notifications via the LLM.
func SetNotifGenFunc(fn func() (map[string]string, error)) {
	notifGenFunc = fn
}

// SetNotifDir sets the notification directory for hook installation.
func SetNotifDir(dir string) {
	notifDir = dir
}

// BroadcastNotificationsReady sends a notifications_updated event to all PWA observers.
func BroadcastNotificationsReady() {
	if reg != nil {
		reg.broadcastEvent(map[string]interface{}{
			"type": "notifications_updated",
		})
	}
}

// interimPhrases are the fixed phrases spoken while the agent searches.
var interimPhrases = []string{
	"Let me look that up.",
	"Give me a moment.",
	"Let me search for that.",
	"One moment while I check.",
}

// PreCacheInterimPhrases pre-generates TTS audio for the fixed interim phrases
// so they can be played instantly during talk mode without waiting for synthesis.
func PreCacheInterimPhrases() {
	funcMu.RLock()
	speakFn := ttsFunc
	funcMu.RUnlock()
	if speakFn == nil {
		return
	}

	voice := ttsVoice
	if voice == "" {
		voice = "default"
	}

	cache := make(map[string]string, len(interimPhrases))
	for _, phrase := range interimPhrases {
		audio, err := speakFn(phrase, voice, "English")
		if err != nil {
			log.Printf("Failed to pre-cache interim phrase %q: %v", phrase, err)
			continue
		}
		cache[phrase] = base64.StdEncoding.EncodeToString(audio)
	}

	interimCacheMu.Lock()
	interimCache = cache
	interimCacheMu.Unlock()
	log.Printf("Pre-cached %d interim phrases", len(cache))
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
	mux.HandleFunc("/tts-voice", handleTTSVoice)
	mux.HandleFunc("/tts-preview", handleTTSPreview)
	mux.HandleFunc("/notifications", handleNotifications)
	mux.HandleFunc("/notifications/dismiss", handleNotifDismiss)
	mux.HandleFunc("/notifications/dismiss-all", handleNotifDismissAll)
	mux.HandleFunc("/notifications/test", handleNotifTest)
	mux.HandleFunc("/notifications/submit", handleNotifSubmit)
	mux.HandleFunc("/hooks/status", handleHookStatus)
	mux.HandleFunc("/hooks/install", handleHookInstall)
	mux.HandleFunc("/hooks/uninstall", handleHookUninstall)
	mux.HandleFunc("/hooks/question", handleHookQuestion)
	mux.HandleFunc("/question/answer", handleQuestionAnswer)
	mux.HandleFunc("/questions", handleListQuestions)

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

	mode := r.FormValue("mode")
	if mode == "" {
		mode = "relay"
	}

	// In relay mode, target is required
	target := r.FormValue("target")
	if mode == "relay" && target == "" {
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

	log.Printf("Received audio: %s, size: %d, mode: %s, target: %s", header.Filename, len(audioData), mode, target)

	// 1. Transcribe audio
	funcMu.RLock()
	transcribeFn := sttFunc
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

	// If whisper detected no speech, return early
	if isBlankTranscription(rawText) {
		log.Printf("No speech detected, not sending")
		writeJSON(w, map[string]interface{}{
			"success":  false,
			"rawText":  rawText,
			"noSpeech": true,
			"sttMs":    sttMs,
			"mode":     mode,
		})
		return
	}

	sessionId := r.FormValue("sessionId")

	// Branch on mode
	if mode == "talk" {
		handleTalkMode(w, rawText, sttMs, sessionId)
		return
	}

	// --- Relay mode (existing behavior) ---
	funcMu.RLock()
	cleanupFn := llmFunc
	funcMu.RUnlock()

	// 2. Clean up text with LLM
	cleanedText := rawText
	summary := ""
	var llmMs int64
	if cleanupFn != nil {
		llmStart := time.Now()
		cleaned, sum, err := cleanupFn(rawText)
		llmMs = time.Since(llmStart).Milliseconds()
		if err != nil {
			log.Printf("LLM cleanup failed (%dms), using raw text: %v", llmMs, err)
		} else {
			cleanedText = cleaned
			summary = sum
		}
	}
	log.Printf("Cleaned text (%dms): %s (summary: %s)", llmMs, cleanedText, summary)

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

	// 4. TTS feedback — returned inline so only the requesting client gets it
	var ttsB64 string
	funcMu.RLock()
	speakFn := ttsFunc
	funcMu.RUnlock()
	if speakFn != nil {
		responseText := buildTTSResponse(cleanedText, summary, target)
		voice := ttsVoice
		if voice == "" {
			voice = "default"
		}
		ttsStart := time.Now()
		ttsAudio, err := speakFn(responseText, voice, "English")
		ttsMs := time.Since(ttsStart).Milliseconds()
		if err != nil {
			log.Printf("TTS failed (%dms): %v", ttsMs, err)
		} else {
			ttsB64 = base64.StdEncoding.EncodeToString(ttsAudio)
			log.Printf("TTS: %d bytes (%dms)", len(ttsAudio), ttsMs)
		}
	}

	resp := map[string]interface{}{
		"success":     true,
		"mode":        "relay",
		"rawText":     rawText,
		"cleanedText": cleanedText,
		"target":      target,
		"sttMs":       sttMs,
		"llmMs":       llmMs,
	}
	if ttsB64 != "" {
		resp["ttsAudio"] = ttsB64
	}
	writeJSON(w, resp)
}

// handleTalkMode runs the agent on transcribed text and returns the response with TTS audio.
// Progress events (searching, interim audio) are pushed via WebSocket to observers.
func handleTalkMode(w http.ResponseWriter, rawText string, sttMs int64, sessionId string) {
	funcMu.RLock()
	agentFn := agentFunc
	speakFn := ttsFunc
	funcMu.RUnlock()

	if agentFn == nil {
		writeJSONError(w, "Talk mode not available (agent not initialized)", http.StatusServiceUnavailable)
		return
	}

	// Send transcription text to the requesting session only
	reg.sendToSession(sessionId, map[string]interface{}{
		"type":  "agent_status",
		"state": "transcribed",
		"text":  rawText,
	})

	// Track whether we've sent an interim spoken response
	interimSent := false

	// Detailed timing breakdown
	type timingEntry struct {
		Label string `json:"label"`
		Ms    int64  `json:"ms"`
	}
	var timings []timingEntry
	phaseStart := time.Now()

	agentStart := time.Now()
	agentResponse, err := agentFn(rawText, func(state, detail string) {
		elapsed := time.Since(phaseStart).Milliseconds()
		phaseStart = time.Now()

		// Record timing for the phase that just ended
		if state == "searching" {
			if len(timings) == 0 {
				timings = append(timings, timingEntry{"LLM decide", elapsed})
			} else {
				timings = append(timings, timingEntry{"LLM respond", elapsed})
			}
		} else if state == "thinking" {
			label := "Tool"
			if detail != "" {
				label = detail
			}
			timings = append(timings, timingEntry{label, elapsed})
		}

		event := map[string]interface{}{
			"type":  "agent_status",
			"state": state,
		}
		if detail != "" {
			event["detail"] = detail
		}

		// On first "searching" event, use pre-cached interim audio for instant playback
		if state == "searching" && !interimSent {
			interimSent = true
			phrase := interimPhrases[len(rawText)%len(interimPhrases)]
			interimCacheMu.RLock()
			if b64, ok := interimCache[phrase]; ok {
				event["ttsAudio"] = b64
			} else if speakFn != nil {
				// Fallback: synthesize on the fly if cache miss
				voice := ttsVoice
				if voice == "" {
					voice = "default"
				}
				if audio, err := speakFn(phrase, voice, "English"); err == nil {
					event["ttsAudio"] = base64.StdEncoding.EncodeToString(audio)
				}
			}
			interimCacheMu.RUnlock()
		}

		reg.sendToSession(sessionId, event)
	})
	// Record the final LLM response phase
	finalElapsed := time.Since(phaseStart).Milliseconds()
	timings = append(timings, timingEntry{"LLM respond", finalElapsed})

	agentMs := time.Since(agentStart).Milliseconds()
	if err != nil {
		log.Printf("Agent error (%dms): %v", agentMs, err)
		writeJSONError(w, fmt.Sprintf("Agent error: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Agent response (%dms): %s", agentMs, agentResponse)

	// TTS the agent response
	var ttsB64 string
	var ttsMs int64
	if speakFn != nil && agentResponse != "" {
		voice := ttsVoice
		if voice == "" {
			voice = "default"
		}
		ttsStart := time.Now()
		ttsAudio, err := speakFn(agentResponse, voice, "English")
		ttsMs = time.Since(ttsStart).Milliseconds()
		if err != nil {
			log.Printf("TTS failed (%dms): %v", ttsMs, err)
		} else {
			ttsB64 = base64.StdEncoding.EncodeToString(ttsAudio)
			log.Printf("TTS: %d bytes (%dms)", len(ttsAudio), ttsMs)
		}
	}
	timings = append(timings, timingEntry{"TTS", ttsMs})

	resp := map[string]interface{}{
		"success":       true,
		"mode":          "talk",
		"rawText":       rawText,
		"agentResponse": agentResponse,
		"sttMs":         sttMs,
		"agentMs":       agentMs,
		"ttsMs":         ttsMs,
		"timings":       timings,
	}
	if ttsB64 != "" {
		resp["ttsAudio"] = ttsB64
	}
	writeJSON(w, resp)
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
				Type      string `json:"type"`
				Name      string `json:"name"`
				SessionId string `json:"sessionId"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("Invalid WebSocket message: %v", err)
				continue
			}

			if msg.Type == "register" && msg.Name != "" {
				sessionNum := reg.register(msg.Name, conn)
				resp, _ := json.Marshal(map[string]interface{}{
					"type":    "registered",
					"name":    msg.Name,
					"session": sessionNum,
				})
				conn.WriteMessage(websocket.TextMessage, resp)
			} else if msg.Type == "observe" {
				reg.addObserver(conn, msg.SessionId)
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

	// Dev mode QR code section
	devSection := ""
	if devURL != "" {
		devPng, err := qrcode.Encode(devURL, qrcode.Medium, 320)
		if err == nil {
			devB64 := base64.StdEncoding.EncodeToString(devPng)
			devSection = fmt.Sprintf(`<div class="dev-section">
      <h2>Dev Mode</h2>
      <p class="label">Vite dev server with live reload</p>
      <img src="data:image/png;base64,%s" width="200" height="200" alt="Dev QR Code">
      <div class="url">%s</div>
    </div>`, devB64, devURL)
		}
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
  .dev-section { margin-top: 2rem; padding-top: 1.5rem; border-top: 1px solid rgba(255,165,0,0.3); }
  .dev-section h2 { font-size: 1.1rem; color: #ffa500; margin-bottom: 0.5rem; }
  .setup-hint { margin-top: 2rem; padding: 1rem; background: rgba(255,255,255,0.05); border-radius: 10px; color: #888; font-size: 0.85rem; line-height: 1.5; }
  .setup-hint strong { color: #ccc; }
</style></head>
<body><div class="card">
  <h1>Voice Relay</h1>
  <p class="subtitle">Scan to open the web app on your phone</p>
  <img src="data:image/png;base64,%s" width="280" height="280" alt="QR Code">
  <div class="url">%s</div>
  %s
  %s
  <div class="setup-hint">
    <strong>Connecting another computer?</strong><br>
    Install Voice Relay, then enter the connection code when prompted during setup.
  </div>
</div></body></html>`, b64, url, codeSection, devSection)
}

// buildTTSResponse creates a natural-sounding spoken confirmation.
func buildTTSResponse(message, summary, target string) string {
	// Use the LLM summary if available, otherwise fall back to message snippet
	topic := summary
	if topic == "" {
		topic = message
		if len(topic) > 60 {
			if i := strings.LastIndex(topic[:60], " "); i > 30 {
				topic = topic[:i]
			} else {
				topic = topic[:60]
			}
		}
	}

	// Vary the phrasing so it doesn't feel robotic
	phrases := []string{
		fmt.Sprintf("Got it, sent your note about %s to %s.", topic, target),
		fmt.Sprintf("Done! Your message about %s is on its way to %s.", topic, target),
		fmt.Sprintf("All set. Sent %s the note about %s.", target, topic),
		fmt.Sprintf("Sent! %s will get your message about %s.", target, topic),
	}

	return phrases[len(message)%len(phrases)]
}

func handleTTSVoice(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, map[string]string{"voice": ttsVoice})
	case "POST":
		var body struct {
			Voice string `json:"voice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Voice == "" {
			writeJSONError(w, "Missing voice name", http.StatusBadRequest)
			return
		}
		funcMu.RLock()
		changeFn := ttsChangeFunc
		funcMu.RUnlock()
		if changeFn == nil {
			writeJSONError(w, "TTS not available", http.StatusServiceUnavailable)
			return
		}
		if err := changeFn(body.Voice); err != nil {
			writeJSONError(w, fmt.Sprintf("Failed to switch voice: %v", err), http.StatusInternalServerError)
			return
		}
		ttsVoice = body.Voice
		writeJSON(w, map[string]string{"voice": ttsVoice})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleTTSPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Text  string `json:"text"`
		Voice string `json:"voice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	text := body.Text
	if text == "" {
		text = "Hello, this is a preview of my voice."
	}
	voice := body.Voice
	if voice == "" {
		voice = ttsVoice
	}

	funcMu.RLock()
	previewFn := ttsPreviewFunc
	funcMu.RUnlock()
	if previewFn == nil {
		writeJSONError(w, "TTS not available", http.StatusServiceUnavailable)
		return
	}

	audioData, err := previewFn(text, voice)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("TTS failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(audioData)))
	w.Write(audioData)
}

func handleNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if notifWatcher == nil {
		writeJSON(w, []interface{}{})
		return
	}
	writeJSON(w, notifWatcher.ListProcessed())
}

func handleNotifDismiss(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if notifWatcher == nil {
		writeJSONError(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeJSONError(w, "Missing id", http.StatusBadRequest)
		return
	}
	if err := notifWatcher.Dismiss(body.ID); err != nil {
		writeJSONError(w, fmt.Sprintf("Dismiss failed: %v", err), http.StatusInternalServerError)
		return
	}
	BroadcastNotificationsReady()
	writeJSON(w, map[string]bool{"ok": true})
}

func handleNotifDismissAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if notifWatcher == nil {
		writeJSONError(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}
	if err := notifWatcher.DismissAll(); err != nil {
		writeJSONError(w, fmt.Sprintf("Dismiss all failed: %v", err), http.StatusInternalServerError)
		return
	}
	BroadcastNotificationsReady()
	writeJSON(w, map[string]bool{"ok": true})
}

func handleNotifSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if notifWatcher == nil {
		writeJSONError(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSONError(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Validate JSON and extract ID
	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSONError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	id := parsed.ID
	if id == "" {
		id = fmt.Sprintf("remote-%d", time.Now().UnixMilli())
	}

	if err := notifWatcher.SubmitRaw(id, body); err != nil {
		writeJSONError(w, fmt.Sprintf("Submit failed: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Received forwarded notification: %s", id)
	writeJSON(w, map[string]interface{}{"ok": true, "id": id})
}

func handleNotifTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if notifWatcher == nil {
		writeJSONError(w, "Notifications not available", http.StatusServiceUnavailable)
		return
	}
	if notifGenFunc == nil {
		writeJSONError(w, "LLM not available", http.StatusServiceUnavailable)
		return
	}

	fields, err := notifGenFunc()
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Generate failed: %v", err), http.StatusInternalServerError)
		return
	}

	if err := notifWatcher.Submit(fields); err != nil {
		writeJSONError(w, fmt.Sprintf("Submit failed: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Test notification submitted: %s", fields["title"])
	writeJSON(w, map[string]interface{}{"ok": true, "title": fields["title"]})
}

// handleHookQuestion receives an AskUserQuestion from a PreToolUse hook script.
// It stores the question and broadcasts it to all PWA observers.
func handleHookQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		ID          string         `json:"id"`
		ReplyTarget string         `json:"reply_target"`
		Questions   []QuestionItem `json:"questions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if body.ID == "" {
		body.ID = fmt.Sprintf("q-%d", time.Now().UnixMilli())
	}

	pq := &PendingQuestion{
		ID:          body.ID,
		ReplyTarget: body.ReplyTarget,
		Questions:   body.Questions,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	pendingQuestionsMu.Lock()
	pendingQuestions[pq.ID] = pq
	pendingQuestionsMu.Unlock()

	log.Printf("Question received: %s (target=%s, %d questions)", pq.ID, pq.ReplyTarget, len(pq.Questions))

	// Broadcast to all PWA observers
	if reg != nil {
		reg.broadcastEvent(map[string]interface{}{
			"type":     "question",
			"question": pq,
		})
	}

	writeJSON(w, map[string]interface{}{"ok": true, "id": pq.ID})
}

// handleQuestionAnswer receives an answer from the PWA and routes it to the cc-wrapper.
func handleQuestionAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		QuestionID string `json:"question_id"`
		Index      int    `json:"index"`      // option index to select
		OtherText  string `json:"other_text"`  // if "Other" was chosen
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	pendingQuestionsMu.Lock()
	pq, ok := pendingQuestions[body.QuestionID]
	if ok {
		pq.Answered = true
	}
	pendingQuestionsMu.Unlock()

	if !ok {
		writeJSONError(w, "Question not found", http.StatusNotFound)
		return
	}

	// Send the selection to the cc-wrapper via registry
	if pq.ReplyTarget == "" {
		writeJSONError(w, "No reply target for this question", http.StatusBadRequest)
		return
	}

	if !reg.sendSelect(pq.ReplyTarget, body.Index, body.OtherText) {
		writeJSONError(w, fmt.Sprintf("Target '%s' not connected", pq.ReplyTarget), http.StatusNotFound)
		return
	}

	log.Printf("Question %s answered: index=%d other=%q -> %s", body.QuestionID, body.Index, body.OtherText, pq.ReplyTarget)

	// Broadcast dismissal so PWA removes the question card
	if reg != nil {
		reg.broadcastEvent(map[string]interface{}{
			"type":        "question_answered",
			"question_id": body.QuestionID,
		})
	}

	// Clean up after a short delay (keep it around briefly for late observers)
	go func() {
		time.Sleep(5 * time.Second)
		pendingQuestionsMu.Lock()
		delete(pendingQuestions, body.QuestionID)
		pendingQuestionsMu.Unlock()
	}()

	writeJSON(w, map[string]interface{}{"ok": true})
}

// handleListQuestions returns all pending (unanswered) questions.
func handleListQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pendingQuestionsMu.RLock()
	var result []*PendingQuestion
	for _, pq := range pendingQuestions {
		if !pq.Answered {
			result = append(result, pq)
		}
	}
	pendingQuestionsMu.RUnlock()

	if result == nil {
		result = []*PendingQuestion{}
	}

	writeJSON(w, result)
}

func handleHookStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	installed, scriptPath := hooks.Status()
	writeJSON(w, map[string]interface{}{
		"installed":  installed,
		"scriptPath": scriptPath,
	})
}

func handleHookInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if notifDir == "" {
		writeJSONError(w, "Notification directory not configured", http.StatusServiceUnavailable)
		return
	}
	if err := hooks.Install(notifDir); err != nil {
		writeJSONError(w, fmt.Sprintf("Install failed: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Claude Code hook installed")
	writeJSON(w, map[string]bool{"ok": true})
}

func handleHookUninstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := hooks.Uninstall(); err != nil {
		writeJSONError(w, fmt.Sprintf("Uninstall failed: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Claude Code hook uninstalled")
	writeJSON(w, map[string]bool{"ok": true})
}

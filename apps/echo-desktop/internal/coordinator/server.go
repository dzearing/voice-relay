package coordinator

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var (
	reg      *registry
	sttFunc  func(audioData []byte, filename string) (string, error)
	llmFunc  func(rawText string) (string, error)
	funcMu    sync.RWMutex
)

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

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/machines", handleMachines)
	mux.HandleFunc("/transcribe", handleTranscribe)
	mux.HandleFunc("/send-text", handleSendText)
	mux.HandleFunc("/ws", handleWebSocket)

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

	rawText, err := transcribeFn(audioData, header.Filename)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Transcription error: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Raw transcription: %s", rawText)

	// 2. Clean up text with LLM
	cleanedText := rawText
	if cleanupFn != nil {
		cleaned, err := cleanupFn(rawText)
		if err != nil {
			log.Printf("LLM cleanup failed, using raw text: %v", err)
		} else {
			cleanedText = cleaned
		}
	}
	log.Printf("Cleaned text: %s", cleanedText)

	// 3. Send to target
	if !reg.sendText(target, cleanedText) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":       fmt.Sprintf("Target machine '%s' not connected", target),
			"rawText":     rawText,
			"cleanedText": cleanedText,
		})
		return
	}

	writeJSON(w, map[string]interface{}{
		"success":     true,
		"rawText":     rawText,
		"cleanedText": cleanedText,
		"target":      target,
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

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

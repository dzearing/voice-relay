package coordinator

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type echoService struct {
	Name        string    `json:"name"`
	Conn        *websocket.Conn `json:"-"`
	ConnectedAt time.Time `json:"connectedAt"`
	Session     int       `json:"session,omitempty"`
}

type registry struct {
	mu          sync.RWMutex
	services    map[string]*echoService
	observers   map[*websocket.Conn]string // value = sessionId
	nextSession int                         // monotonic counter for claude sessions
}

func newRegistry() *registry {
	return &registry{
		services:  make(map[string]*echoService),
		observers: make(map[*websocket.Conn]string),
	}
}

func (r *registry) register(name string, conn *websocket.Conn) int {
	r.mu.Lock()

	// Close existing connection if any; preserve session number
	existingSession := 0
	if existing, ok := r.services[name]; ok {
		existingSession = existing.Session
		existing.Conn.Close()
	}

	svc := &echoService{
		Name:        name,
		Conn:        conn,
		ConnectedAt: time.Now(),
	}

	// Assign session number to claude instances (reuse on re-registration)
	if strings.Contains(name, "-claude") {
		if existingSession > 0 {
			svc.Session = existingSession
		} else {
			r.nextSession++
			svc.Session = r.nextSession
		}
	}

	r.services[name] = svc
	sessionNum := svc.Session
	log.Printf("Echo service registered: %s (session=%d)", name, sessionNum)
	r.mu.Unlock()

	r.broadcastMachines()
	return sessionNum
}

func (r *registry) unregister(conn *websocket.Conn) {
	r.mu.Lock()

	// Check if it's an observer
	if _, ok := r.observers[conn]; ok {
		delete(r.observers, conn)
		r.mu.Unlock()
		log.Printf("Observer disconnected")
		return
	}

	removed := false
	for name, svc := range r.services {
		if svc.Conn == conn {
			delete(r.services, name)
			log.Printf("Echo service unregistered: %s", name)
			removed = true
			break
		}
	}
	r.mu.Unlock()

	if removed {
		r.broadcastMachines()
	}
}

func (r *registry) addObserver(conn *websocket.Conn, sessionId string) {
	r.mu.Lock()
	r.observers[conn] = sessionId
	r.mu.Unlock()
	log.Printf("Observer connected (session=%s)", sessionId)

	// Send current machine list immediately
	r.sendMachinesTo(conn)
}

func (r *registry) broadcastMachines() {
	r.mu.RLock()
	machines := r.listLocked()
	observers := make([]*websocket.Conn, 0, len(r.observers))
	for conn := range r.observers {
		observers = append(observers, conn)
	}
	r.mu.RUnlock()

	msg, _ := json.Marshal(map[string]interface{}{
		"type":     "machines",
		"machines": machines,
	})

	for _, conn := range observers {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("Failed to send to observer: %v", err)
		}
	}
}

func (r *registry) sendMachinesTo(conn *websocket.Conn) {
	r.mu.RLock()
	machines := r.listLocked()
	r.mu.RUnlock()

	msg, _ := json.Marshal(map[string]interface{}{
		"type":     "machines",
		"machines": machines,
	})
	conn.WriteMessage(websocket.TextMessage, msg)
}

type machineInfo struct {
	Name        string    `json:"name"`
	ConnectedAt time.Time `json:"connectedAt"`
}

func (r *registry) list() []machineInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listLocked()
}

// listLocked returns the machine list. Caller must hold at least r.mu.RLock().
func (r *registry) listLocked() []machineInfo {
	result := make([]machineInfo, 0, len(r.services))
	for _, svc := range r.services {
		result = append(result, machineInfo{
			Name:        svc.Name,
			ConnectedAt: svc.ConnectedAt,
		})
	}
	return result
}

// broadcastAudio sends base64-encoded WAV audio to all observer connections.
func (r *registry) broadcastAudio(wavData []byte) {
	r.mu.RLock()
	observers := make([]*websocket.Conn, 0, len(r.observers))
	for conn := range r.observers {
		observers = append(observers, conn)
	}
	r.mu.RUnlock()

	b64 := base64.StdEncoding.EncodeToString(wavData)
	msg, _ := json.Marshal(map[string]string{
		"type": "audio",
		"data": b64,
	})

	for _, conn := range observers {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("Failed to send audio to observer: %v", err)
		}
	}
}

// broadcastEvent sends a JSON event to all observer connections.
func (r *registry) broadcastEvent(data map[string]interface{}) {
	msg, err := json.Marshal(data)
	if err != nil {
		return
	}

	r.mu.RLock()
	observers := make([]*websocket.Conn, 0, len(r.observers))
	for conn := range r.observers {
		observers = append(observers, conn)
	}
	r.mu.RUnlock()

	for _, conn := range observers {
		conn.WriteMessage(websocket.TextMessage, msg)
	}
}

// sendToSession sends a JSON event only to the observer with the matching sessionId.
func (r *registry) sendToSession(sessionId string, data map[string]interface{}) {
	msg, err := json.Marshal(data)
	if err != nil {
		return
	}

	r.mu.RLock()
	var targets []*websocket.Conn
	for conn, sid := range r.observers {
		if sid == sessionId {
			targets = append(targets, conn)
		}
	}
	r.mu.RUnlock()

	for _, conn := range targets {
		conn.WriteMessage(websocket.TextMessage, msg)
	}
}

func (r *registry) sendText(name, text string) bool {
	r.mu.RLock()
	svc, ok := r.services[name]
	r.mu.RUnlock()

	if !ok {
		return false
	}

	msg := map[string]string{
		"type":    "text",
		"content": text,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}

	if err := svc.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return false
	}

	return true
}

// sendSelect sends a "select" message to a cc-wrapper device, instructing it
// to navigate an AskUserQuestion TUI by pressing down-arrow `index` times
// then Enter. If otherText is non-empty, it types that after selecting "Other".
func (r *registry) sendSelect(name string, index int, otherText string) bool {
	r.mu.RLock()
	svc, ok := r.services[name]
	r.mu.RUnlock()

	if !ok {
		return false
	}

	msg := map[string]interface{}{
		"type":  "select",
		"index": index,
	}
	if otherText != "" {
		msg["content"] = otherText
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}

	if err := svc.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return false
	}

	return true
}

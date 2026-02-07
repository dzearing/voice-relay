package coordinator

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type echoService struct {
	Name        string    `json:"name"`
	Conn        *websocket.Conn `json:"-"`
	ConnectedAt time.Time `json:"connectedAt"`
}

type registry struct {
	mu        sync.RWMutex
	services  map[string]*echoService
	observers map[*websocket.Conn]struct{}
}

func newRegistry() *registry {
	return &registry{
		services:  make(map[string]*echoService),
		observers: make(map[*websocket.Conn]struct{}),
	}
}

func (r *registry) register(name string, conn *websocket.Conn) {
	r.mu.Lock()

	// Close existing connection if any
	if existing, ok := r.services[name]; ok {
		existing.Conn.Close()
	}

	r.services[name] = &echoService{
		Name:        name,
		Conn:        conn,
		ConnectedAt: time.Now(),
	}
	log.Printf("Echo service registered: %s", name)
	r.mu.Unlock()

	r.broadcastMachines()
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

func (r *registry) addObserver(conn *websocket.Conn) {
	r.mu.Lock()
	r.observers[conn] = struct{}{}
	r.mu.Unlock()
	log.Printf("Observer connected")

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

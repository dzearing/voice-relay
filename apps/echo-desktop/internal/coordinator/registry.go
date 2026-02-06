package coordinator

import (
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
	mu       sync.RWMutex
	services map[string]*echoService
}

func newRegistry() *registry {
	return &registry{
		services: make(map[string]*echoService),
	}
}

func (r *registry) register(name string, conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()

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
}

func (r *registry) unregister(conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, svc := range r.services {
		if svc.Conn == conn {
			delete(r.services, name)
			log.Printf("Echo service unregistered: %s", name)
			return
		}
	}
}

type machineInfo struct {
	Name        string    `json:"name"`
	ConnectedAt time.Time `json:"connectedAt"`
}

func (r *registry) list() []machineInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]machineInfo, 0, len(r.services))
	for _, svc := range r.services {
		result = append(result, machineInfo{
			Name:        svc.Name,
			ConnectedAt: svc.ConnectedAt,
		})
	}
	return result
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

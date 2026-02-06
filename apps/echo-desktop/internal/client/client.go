package client

import (
	"log"
	"time"

	"github.com/atotto/clipboard"
	"github.com/gorilla/websocket"

	"github.com/voice-relay/echo-desktop/internal/keyboard"
)

// Message represents a WebSocket message exchanged with the coordinator.
type Message struct {
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

// StatusFunc is called to update the UI with connection status.
type StatusFunc func(connected bool, status string)

// Client manages the WebSocket connection to the coordinator.
type Client struct {
	Name           string
	CoordinatorURL string
	OnStatus       StatusFunc
	Reconnect      chan bool

	conn       *websocket.Conn
	lastText   string
	lastTextAt time.Time
}

// New creates a new echo client.
func New(name, coordinatorURL string, onStatus StatusFunc) *Client {
	return &Client{
		Name:           name,
		CoordinatorURL: coordinatorURL,
		OnStatus:       onStatus,
		Reconnect:      make(chan bool, 1),
	}
}

// Run starts the connection manager loop. It blocks forever, reconnecting as needed.
func (c *Client) Run() {
	for {
		c.connect()

		select {
		case <-c.Reconnect:
			log.Println("Manual reconnect requested")
		case <-time.After(5 * time.Second):
		}
	}
}

// Close closes the current connection if any.
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// TriggerReconnect requests an immediate reconnect.
func (c *Client) TriggerReconnect() {
	select {
	case c.Reconnect <- true:
	default:
	}
}

func (c *Client) connect() {
	c.OnStatus(false, "Connecting...")

	conn, _, err := websocket.DefaultDialer.Dial(c.CoordinatorURL, nil)
	if err != nil {
		log.Printf("Connection failed: %v", err)
		c.OnStatus(false, "Connection failed")
		return
	}
	c.conn = conn

	msg := Message{Type: "register", Name: c.Name}
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("Registration failed: %v", err)
		conn.Close()
		c.OnStatus(false, "Registration failed")
		return
	}

	c.OnStatus(true, "Connected as "+c.Name)
	log.Printf("Connected as %s", c.Name)

	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Read error: %v", err)
			break
		}

		switch msg.Type {
		case "registered":
			log.Printf("Registered as: %s", msg.Name)
		case "text":
			c.handleText(msg.Content)
		}
	}

	conn.Close()
	c.conn = nil
	c.OnStatus(false, "Disconnected")
}

func (c *Client) handleText(text string) {
	if text == "" {
		return
	}

	log.Printf("Received: %s", text)

	if text == c.lastText && time.Since(c.lastTextAt) < 2*time.Second {
		log.Println("Ignoring duplicate message")
		return
	}
	c.lastText = text
	c.lastTextAt = time.Now()

	if err := clipboard.WriteAll(text); err != nil {
		log.Printf("Clipboard error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if err := keyboard.Paste(); err != nil {
		log.Printf("Paste error: %v", err)
	}
}

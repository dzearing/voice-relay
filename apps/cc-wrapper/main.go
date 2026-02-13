package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

// Message matches the coordinator WebSocket protocol.
type Message struct {
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
	Session int    `json:"session,omitempty"`
	Index   int    `json:"index,omitempty"` // option index for "select" type
}

func main() {
	wsURL := flag.String("ws", "ws://localhost:53937/ws", "coordinator WebSocket URL")
	name := flag.String("name", defaultName(), "device name for registration")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: cc-wrapper [--ws URL] [--name NAME] <command> [args...]")
		os.Exit(1)
	}

	// Redirect log output to file so it doesn't corrupt the TUI
	logFile, err := os.OpenFile("cc-wrapper.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	} else {
		log.SetOutput(io.Discard)
	}

	// Expose wrapper name to child process so hooks can tag notifications
	os.Setenv("CC_WRAPPER_NAME", *name)

	// Pre-register with coordinator to get session number BEFORE spawning
	// the child process, so CC_SESSION is inherited and the terminal title
	// is set before raw mode / PTY output starts.
	session := quickRegister(*wsURL, *name)
	if session > 0 {
		os.Setenv("CC_SESSION", strconv.Itoa(session))
		fmt.Fprintf(os.Stdout, "\x1b]2;CC #%d\x07", session)
	}

	// Create PTY and spawn the command.
	p, err := spawnPTY(args)
	if err != nil {
		// Fatal before raw mode — print to stderr is fine
		fmt.Fprintf(os.Stderr, "Failed to spawn PTY: %v\n", err)
		os.Exit(1)
	}
	defer p.Close()

	// Put host terminal in raw mode so key-by-key input works.
	restoreFn := enableRawMode()
	defer restoreFn()

	// PTY stdout -> host stdout
	go func() { io.Copy(os.Stdout, p) }()

	// Host stdin -> PTY stdin
	go func() { io.Copy(p, os.Stdin) }()

	// WebSocket client: connect, register, receive text -> write to PTY
	go wsLoop(*wsURL, *name, p)

	// Forward Ctrl-C to PTY instead of killing wrapper
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		for range sigCh {
			p.Write([]byte{3})
		}
	}()

	// Wait for child to exit
	code := p.Wait()
	restoreFn()
	os.Exit(code)
}

func defaultName() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "PC"
	}
	// Append short random suffix so multiple instances get unique names
	b := make([]byte, 2)
	rand.Read(b)
	return host + "-claude-" + hex.EncodeToString(b)
}

// quickRegister connects to the coordinator, registers, and returns the
// session number. The connection is closed immediately — wsLoop will
// establish the long-lived connection afterwards. Returns 0 on failure.
func quickRegister(url, name string) int {
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		log.Printf("[ws] quick-register connect failed: %v", err)
		return 0
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.WriteJSON(Message{Type: "register", Name: name}); err != nil {
		log.Printf("[ws] quick-register write failed: %v", err)
		return 0
	}

	var msg Message
	if err := conn.ReadJSON(&msg); err != nil {
		log.Printf("[ws] quick-register read failed: %v", err)
		return 0
	}

	log.Printf("[ws] quick-register: session=%d", msg.Session)
	return msg.Session
}

func wsLoop(url, name string, p ptyHandle) {
	for {
		connectAndServe(url, name, p)
		time.Sleep(5 * time.Second)
	}
}

func connectAndServe(url, name string, p ptyHandle) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Printf("[ws] connect failed: %v", err)
		return
	}
	defer conn.Close()

	// Register as echo device
	reg := Message{Type: "register", Name: name}
	if err := conn.WriteJSON(reg); err != nil {
		log.Printf("[ws] register failed: %v", err)
		return
	}
	log.Printf("[ws] registered as %s", name)

	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("[ws] read error: %v", err)
			return
		}

		switch msg.Type {
		case "registered":
			log.Printf("[ws] confirmed: %s (session=%d)", msg.Name, msg.Session)
		case "text":
			if msg.Content == "" {
				continue
			}
			log.Printf("[ws] injecting text (%d chars)", len(msg.Content))
			injectText(p, msg.Content)
		case "select":
			// Navigate AskUserQuestion TUI: arrow-down N times, then Enter.
			// If Content is set, it's an "Other" response: navigate to last
			// option (index), press Enter to select "Other", then type the text.
			log.Printf("[ws] selecting option index=%d content=%q", msg.Index, msg.Content)
			injectSelect(p, msg.Index, msg.Content)
		}
	}
}

// enableRawMode puts stdin into raw mode and returns a restore function.
func enableRawMode() func() {
	restoreFn, err := setRawTerminal()
	if err != nil {
		log.Printf("[term] raw mode unavailable: %v", err)
		return func() {}
	}
	return restoreFn
}

// injectText writes text rune-by-rune into the PTY with a small delay
// to let Claude Code's TUI process each character, then sends \r (Enter).
func injectText(p ptyHandle, text string) {
	buf := make([]byte, 4)
	for _, r := range text {
		n := utf8.EncodeRune(buf, r)
		p.Write(buf[:n])
		time.Sleep(5 * time.Millisecond)
	}
	// In raw-mode terminals, Enter is \r (carriage return), not \n
	time.Sleep(20 * time.Millisecond)
	p.Write([]byte{'\r'})
}

// injectSelect navigates an AskUserQuestion TUI picker.
// It sends `index` down-arrow presses to reach the desired option, then Enter.
// If `otherText` is non-empty, the selected option is "Other" — after pressing
// Enter on it, we type the custom text and press Enter again.
func injectSelect(p ptyHandle, index int, otherText string) {
	downArrow := []byte("\x1b[B") // ANSI escape: cursor down
	for i := 0; i < index; i++ {
		p.Write(downArrow)
		time.Sleep(30 * time.Millisecond)
	}
	// Press Enter to select the option
	time.Sleep(50 * time.Millisecond)
	p.Write([]byte{'\r'})

	if otherText != "" {
		// Wait for "Other" text input to appear, then type
		time.Sleep(200 * time.Millisecond)
		injectText(p, otherText)
	}
}

// ptyHandle abstracts the PTY interface across platforms.
type ptyHandle interface {
	io.ReadWriteCloser
	Wait() int
}

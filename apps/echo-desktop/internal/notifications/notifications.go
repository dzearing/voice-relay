package notifications

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Notification represents a notification with optional TTS audio.
type Notification struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Summary      string `json:"summary"`
	Details      string `json:"details,omitempty"`
	Priority     string `json:"priority,omitempty"`
	Source       string `json:"source,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	ProcessedAt  string `json:"processed_at,omitempty"`
	SummaryAudio string `json:"summary_audio,omitempty"`
	DetailsAudio string `json:"details_audio,omitempty"`
}

// TTSFunc synthesizes text to WAV audio bytes.
type TTSFunc func(text, voice, language string) ([]byte, error)

// Watcher polls a pending directory for notification JSON files and processes them.
type Watcher struct {
	baseDir    string
	ttsFunc    TTSFunc
	ttsVoice   func() string // returns current voice name
	onReady    func()        // called after processing a notification
	stopCh     chan struct{}
	mu         sync.Mutex
}

// NewWatcher creates a new notification watcher.
func NewWatcher(baseDir string, tts TTSFunc, voiceFn func() string, onReady func()) *Watcher {
	return &Watcher{
		baseDir:  baseDir,
		ttsFunc:  tts,
		ttsVoice: voiceFn,
		onReady:  onReady,
		stopCh:   make(chan struct{}),
	}
}

// EnsureDirs creates the notification pipeline directories.
func (w *Watcher) EnsureDirs() error {
	for _, dir := range []string{"pending", "processing", "processed", "archived"} {
		if err := os.MkdirAll(filepath.Join(w.baseDir, dir), 0755); err != nil {
			return err
		}
	}
	return nil
}

// Submit writes a notification JSON into the pending directory for processing.
func (w *Watcher) Submit(fields map[string]string) error {
	id := fmt.Sprintf("test-%d", time.Now().UnixMilli())
	n := Notification{
		ID:        id,
		Title:     fields["title"],
		Summary:   fields["summary"],
		Details:   fields["details"],
		Priority:  fields["priority"],
		Source:    fields["source"],
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.baseDir, "pending", id+".json"), data, 0644)
}

// Start begins the polling loop. Call in a goroutine.
func (w *Watcher) Start() {
	// Recover any stale files in processing/ back to pending/
	w.recoverStale()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processPending()
		}
	}
}

// Stop halts the polling loop.
func (w *Watcher) Stop() {
	close(w.stopCh)
}

// recoverStale moves files from processing/ back to pending/.
func (w *Watcher) recoverStale() {
	processingDir := filepath.Join(w.baseDir, "processing")
	pendingDir := filepath.Join(w.baseDir, "pending")

	entries, err := os.ReadDir(processingDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		src := filepath.Join(processingDir, e.Name())
		dst := filepath.Join(pendingDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			log.Printf("notifications: failed to recover %s: %v", e.Name(), err)
		} else {
			log.Printf("notifications: recovered stale %s to pending", e.Name())
		}
	}
}

// processPending scans the pending directory and processes files serially.
func (w *Watcher) processPending() {
	pendingDir := filepath.Join(w.baseDir, "pending")
	processingDir := filepath.Join(w.baseDir, "processing")
	processedDir := filepath.Join(w.baseDir, "processed")

	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		src := filepath.Join(pendingDir, e.Name())
		mid := filepath.Join(processingDir, e.Name())

		// Move to processing
		if err := os.Rename(src, mid); err != nil {
			log.Printf("notifications: failed to move %s to processing: %v", e.Name(), err)
			continue
		}

		// Read and parse
		data, err := os.ReadFile(mid)
		if err != nil {
			log.Printf("notifications: failed to read %s: %v", e.Name(), err)
			continue
		}

		var notif Notification
		if err := json.Unmarshal(data, &notif); err != nil {
			log.Printf("notifications: invalid JSON in %s: %v", e.Name(), err)
			// Move bad file to archived so it doesn't block the pipeline
			os.Rename(mid, filepath.Join(w.baseDir, "archived", e.Name()))
			continue
		}

		// Default ID from filename stem
		if notif.ID == "" {
			notif.ID = strings.TrimSuffix(e.Name(), ".json")
		}

		// Validate required fields
		if notif.Title == "" || notif.Summary == "" {
			log.Printf("notifications: %s missing title or summary, archiving", e.Name())
			os.Rename(mid, filepath.Join(w.baseDir, "archived", e.Name()))
			continue
		}

		// Generate TTS audio
		if w.ttsFunc != nil {
			voice := "default"
			if w.ttsVoice != nil {
				if v := w.ttsVoice(); v != "" {
					voice = v
				}
			}

			if audio, err := w.ttsFunc(notif.Summary, voice, "English"); err == nil {
				notif.SummaryAudio = base64.StdEncoding.EncodeToString(audio)
			} else {
				log.Printf("notifications: TTS failed for summary: %v", err)
			}

			if notif.Details != "" {
				if audio, err := w.ttsFunc(notif.Details, voice, "English"); err == nil {
					notif.DetailsAudio = base64.StdEncoding.EncodeToString(audio)
				} else {
					log.Printf("notifications: TTS failed for details: %v", err)
				}
			}
		}

		notif.ProcessedAt = time.Now().UTC().Format(time.RFC3339)

		// Write processed file
		out, err := json.Marshal(notif)
		if err != nil {
			log.Printf("notifications: failed to marshal %s: %v", e.Name(), err)
			continue
		}

		dst := filepath.Join(processedDir, e.Name())
		if err := os.WriteFile(dst, out, 0644); err != nil {
			log.Printf("notifications: failed to write processed %s: %v", e.Name(), err)
			continue
		}

		// Remove from processing
		os.Remove(mid)
		log.Printf("notifications: processed %s (%s)", e.Name(), notif.Title)

		// Notify listeners
		if w.onReady != nil {
			w.onReady()
		}
	}
}

// ListProcessed returns all processed notifications sorted newest-first.
func (w *Watcher) ListProcessed() []Notification {
	processedDir := filepath.Join(w.baseDir, "processed")

	entries, err := os.ReadDir(processedDir)
	if err != nil {
		return nil
	}

	var notifs []Notification
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(processedDir, e.Name()))
		if err != nil {
			continue
		}

		var n Notification
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}
		notifs = append(notifs, n)
	}

	// Sort newest first by processed_at
	sort.Slice(notifs, func(i, j int) bool {
		return notifs[i].ProcessedAt > notifs[j].ProcessedAt
	})

	return notifs
}

// Dismiss moves a notification from processed to archived.
func (w *Watcher) Dismiss(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	processedDir := filepath.Join(w.baseDir, "processed")
	archivedDir := filepath.Join(w.baseDir, "archived")

	// Find the file with matching ID
	entries, err := os.ReadDir(processedDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(processedDir, e.Name()))
		if err != nil {
			continue
		}

		var n Notification
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}

		if n.ID == id {
			return os.Rename(
				filepath.Join(processedDir, e.Name()),
				filepath.Join(archivedDir, e.Name()),
			)
		}
	}

	return nil
}

// DismissAll moves all processed notifications to archived.
func (w *Watcher) DismissAll() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	processedDir := filepath.Join(w.baseDir, "processed")
	archivedDir := filepath.Join(w.baseDir, "archived")

	entries, err := os.ReadDir(processedDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		os.Rename(
			filepath.Join(processedDir, e.Name()),
			filepath.Join(archivedDir, e.Name()),
		)
	}

	return nil
}

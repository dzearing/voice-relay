package stt

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Engine manages whisper-server as a subprocess and communicates via HTTP.
type Engine struct {
	modelPath  string
	serverPath string
	port       int
	cmd        *exec.Cmd
	apiURL     string
}

// NewEngine creates a new STT engine. It expects the paths to the whisper model
// and the whisper-server binary.
func NewEngine(modelPath, serverPath string, port int) (*Engine, error) {
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("model not found: %s", modelPath)
	}
	if _, err := os.Stat(serverPath); err != nil {
		return nil, fmt.Errorf("whisper-server not found: %s", serverPath)
	}

	e := &Engine{
		modelPath:  modelPath,
		serverPath: serverPath,
		port:       port,
		apiURL:     fmt.Sprintf("http://127.0.0.1:%d", port),
	}

	if err := e.start(); err != nil {
		return nil, fmt.Errorf("failed to start whisper-server: %w", err)
	}

	return e, nil
}

func (e *Engine) start() error {
	args := []string{
		"--model", e.modelPath,
		"--port", fmt.Sprintf("%d", e.port),
		"--host", "127.0.0.1",
	}

	e.cmd = exec.Command(e.serverPath, args...)
	e.cmd.Stdout = os.Stdout
	e.cmd.Stderr = os.Stderr

	// On Windows, hide the console window
	setSysProcAttr(e.cmd)

	if err := e.cmd.Start(); err != nil {
		return err
	}

	log.Printf("whisper-server starting on port %d (pid %d)", e.port, e.cmd.Process.Pid)

	// Wait for server to be ready
	if err := e.waitReady(30 * time.Second); err != nil {
		e.Close()
		return err
	}

	log.Printf("whisper-server ready")
	return nil
}

func (e *Engine) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(e.apiURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("whisper-server did not become ready within %v", timeout)
}

// Transcribe sends audio data to whisper-server and returns the transcribed text.
// Accepts any audio format that whisper-server supports (webm, wav, mp3, etc).
func (e *Engine) Transcribe(audioData []byte, filename string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Request plain text response
	if fw, err := w.CreateFormField("response_format"); err == nil {
		fw.Write([]byte("text"))
	}

	w.Close()

	resp, err := http.Post(e.apiURL+"/inference", w.FormDataContentType(), &buf)
	if err != nil {
		return "", fmt.Errorf("transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("transcription error %d: %s", resp.StatusCode, string(body))
	}

	result, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return strings.TrimSpace(string(result)), nil
}

// Close stops the whisper-server subprocess.
func (e *Engine) Close() {
	if e.cmd != nil && e.cmd.Process != nil {
		log.Printf("Stopping whisper-server (pid %d)", e.cmd.Process.Pid)
		e.cmd.Process.Kill()
		e.cmd.Wait()
	}
}

// ServerBinaryName returns the platform-specific whisper-server binary name.
func ServerBinaryName() string {
	if runtime.GOOS == "windows" {
		return "whisper-server.exe"
	}
	return "whisper-server"
}

// WhisperServerAssetName returns the whisper.cpp release asset name for this platform.
func WhisperServerAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "whisper-bin-x64.zip"
	case "darwin":
		return "whisper-bin-arm64-apple.zip"
	default:
		return "whisper-bin-x64.zip"
	}
}

// WhisperServerPath returns the expected path to the whisper-server binary in the given directory.
func WhisperServerPath(dir string) string {
	return filepath.Join(dir, "bin", ServerBinaryName())
}

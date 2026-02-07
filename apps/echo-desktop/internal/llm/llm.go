package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const systemPrompt = `You are a speech-to-text post-processor. Your ONLY job is to clean up transcribed speech.

STEP 1: Remove filler words (uh, um, like, you know).
STEP 2: When the speaker corrects themselves, ONLY keep the correction:
- "X. I mean Y" = speaker wants Y, not X
- "X, no wait, Y" = speaker wants Y, not X
- "X, actually Y" = speaker wants Y, not X
- "X, no Y" = speaker wants Y, not X
STEP 3: If already clean, return unchanged.

Reply with ONLY the cleaned sentence.`

// Engine manages llama-server as a subprocess for text cleanup.
type Engine struct {
	modelPath  string
	serverPath string
	port       int
	cmd        *exec.Cmd
	apiURL     string
}

// NewEngine creates a new LLM engine using llama-server.
func NewEngine(modelPath, serverPath string, port int) (*Engine, error) {
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("model not found: %s", modelPath)
	}
	if _, err := os.Stat(serverPath); err != nil {
		return nil, fmt.Errorf("llama-server not found: %s", serverPath)
	}

	e := &Engine{
		modelPath:  modelPath,
		serverPath: serverPath,
		port:       port,
		apiURL:     fmt.Sprintf("http://127.0.0.1:%d", port),
	}

	if err := e.start(); err != nil {
		return nil, fmt.Errorf("failed to start llama-server: %w", err)
	}

	return e, nil
}

func (e *Engine) start() error {
	args := []string{
		"--model", e.modelPath,
		"--port", fmt.Sprintf("%d", e.port),
		"--host", "127.0.0.1",
		"--ctx-size", "4096",
	}

	e.cmd = exec.Command(e.serverPath, args...)
	e.cmd.Stdout = os.Stdout
	e.cmd.Stderr = os.Stderr

	setSysProcAttr(e.cmd)

	if err := e.cmd.Start(); err != nil {
		return err
	}

	log.Printf("llama-server starting on port %d (pid %d)", e.port, e.cmd.Process.Pid)

	if err := e.waitReady(60 * time.Second); err != nil {
		e.Close()
		return err
	}

	log.Printf("llama-server ready")
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
	return fmt.Errorf("llama-server did not become ready within %v", timeout)
}

// CleanupText sends raw transcribed text through the LLM for cleanup.
func (e *Engine) CleanupText(rawText string) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": "qwen3",
		"messages": []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: rawText + " /no_think"},
		},
		"max_tokens":  512,
		"temperature": 0.1,
	})

	resp, err := http.Post(e.apiURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("LLM request failed, returning raw text: %v", err)
		return rawText, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("LLM error %d: %s, returning raw text", resp.StatusCode, string(body))
		return rawText, nil
	}

	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("Failed to decode LLM response: %v, returning raw text", err)
		return rawText, nil
	}

	if len(data.Choices) == 0 {
		return rawText, nil
	}

	result := data.Choices[0].Message.Content

	// Strip Qwen3 thinking blocks: <think>...</think>
	if idx := strings.Index(result, "</think>"); idx >= 0 {
		result = result[idx+len("</think>"):]
	}

	result = strings.TrimSpace(result)

	// Remove leading/trailing quotes
	result = strings.Trim(result, "\"'")

	log.Printf("LLM cleanup: %q -> %q", rawText, result)

	if result == "" {
		return rawText, nil
	}
	return result, nil
}

// Close stops the llama-server subprocess.
func (e *Engine) Close() {
	if e.cmd != nil && e.cmd.Process != nil {
		log.Printf("Stopping llama-server (pid %d)", e.cmd.Process.Pid)
		e.cmd.Process.Kill()
		e.cmd.Wait()
	}
}

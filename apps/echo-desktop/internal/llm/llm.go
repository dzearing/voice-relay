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
	"runtime"
	"strings"
	"time"
)

const systemPrompt = `You are a speech-to-text post-processor. You do two things:

1. CLEAN the transcribed text:
   - Remove filler words (uh, um, like, you know)
   - When the speaker corrects themselves, ONLY keep the correction:
     "X. I mean Y" → keep Y. "X, no wait, Y" → keep Y.
   - If already clean, keep unchanged.

2. SUMMARIZE in a few words what the message is about.

Reply with ONLY a JSON object, no other text:
{"cleaned": "the cleaned text", "summary": "2-5 word summary"}

Examples:
Input: "uh can you pick up um some milk on the way home"
Output: {"cleaned": "Can you pick up some milk on the way home?", "summary": "picking up milk"}

Input: "hey the meeting is at 3, no wait, 4 pm"
Output: {"cleaned": "Hey, the meeting is at 4 PM.", "summary": "meeting time update"}`

// chatMessage is a message in the OpenAI chat format.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is a request to the chat completions API.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
}

// chatResponse is a response from the chat completions API.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

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
	// Use half the available CPU threads (min 4) to avoid saturating all cores
	threads := runtime.NumCPU() / 2
	if threads < 4 {
		threads = 4
	}

	args := []string{
		"--model", e.modelPath,
		"--port", fmt.Sprintf("%d", e.port),
		"--host", "127.0.0.1",
		"--ctx-size", "4096",
		"--cache-ram", "0",
		"--jinja",
		"--threads", fmt.Sprintf("%d", threads),
	}

	// Offload all layers to GPU when NVIDIA GPU is available
	if HasNvidiaGPU() {
		args = append(args, "--n-gpu-layers", "99")
		log.Printf("llama-server: GPU offloading enabled (all layers)")
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
// Returns (cleaned text, summary, error).
func (e *Engine) CleanupText(rawText string) (string, string, error) {
	reqBody, _ := json.Marshal(chatRequest{
		Model: "qwen3",
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: rawText + " /no_think"},
		},
		MaxTokens:   512,
		Temperature: 0.1,
	})

	resp, err := http.Post(e.apiURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("LLM request failed, returning raw text: %v", err)
		return rawText, "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("LLM error %d: %s, returning raw text", resp.StatusCode, string(body))
		return rawText, "", nil
	}

	var data chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("Failed to decode LLM response: %v, returning raw text", err)
		return rawText, "", nil
	}

	if len(data.Choices) == 0 {
		return rawText, "", nil
	}

	result := data.Choices[0].Message.Content

	// Strip Qwen3 thinking blocks: <think>...</think>
	if idx := strings.Index(result, "</think>"); idx >= 0 {
		result = result[idx+len("</think>"):]
	}

	result = strings.TrimSpace(result)

	// Try to parse as JSON {"cleaned": "...", "summary": "..."}
	var parsed struct {
		Cleaned string `json:"cleaned"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err == nil && parsed.Cleaned != "" {
		log.Printf("LLM cleanup: %q -> %q (summary: %q)", rawText, parsed.Cleaned, parsed.Summary)
		return parsed.Cleaned, parsed.Summary, nil
	}

	// Fallback: treat the whole response as cleaned text (no summary)
	result = strings.Trim(result, "\"'")
	log.Printf("LLM cleanup (no JSON): %q -> %q", rawText, result)

	if result == "" {
		return rawText, "", nil
	}
	return result, "", nil
}

const notifSummarizePrompt = `You are summarizing a Claude Code assistant response for a voice notification (text-to-speech).

Given the user's request and the assistant's response, generate a JSON object with:
- "title": A short, grammatically correct sentence (max 60 chars) describing what the user asked for. Start with "You asked to..." and use natural phrasing. Examples: "You asked to update the autoplay control", "You asked to fix the login bug", "You asked about deployment options".
- "summary": A 1-2 sentence spoken summary of what Claude did. Write naturally for speech. No markdown, no code, no URLs. Max 200 chars.
- "details": A longer plain-text summary (3-5 sentences) covering the key points. No markdown. Max 800 chars.

Reply with ONLY the JSON object, no other text.`

// SummarizeNotification uses the LLM to generate title/summary/details from raw transcript text.
func (e *Engine) SummarizeNotification(userText, assistantText string) (string, string, string, error) {
	// Truncate assistant text to avoid overwhelming the context
	if len(assistantText) > 3000 {
		assistantText = assistantText[:3000] + "\n..."
	}

	userContent := fmt.Sprintf("USER REQUEST:\n%s\n\nASSISTANT RESPONSE:\n%s /no_think", userText, assistantText)

	reqBody, _ := json.Marshal(chatRequest{
		Model: "qwen3",
		Messages: []chatMessage{
			{Role: "system", Content: notifSummarizePrompt},
			{Role: "user", Content: userContent},
		},
		MaxTokens:   512,
		Temperature: 0.3,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(e.apiURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return "", "", "", fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", "", fmt.Errorf("LLM error %d: %s", resp.StatusCode, string(body))
	}

	var data chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", "", fmt.Errorf("decode failed: %w", err)
	}
	if len(data.Choices) == 0 {
		return "", "", "", fmt.Errorf("no choices returned")
	}

	result := data.Choices[0].Message.Content
	if idx := strings.Index(result, "</think>"); idx >= 0 {
		result = result[idx+len("</think>"):]
	}
	result = strings.TrimSpace(result)

	var parsed struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Details string `json:"details"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return "", "", "", fmt.Errorf("invalid JSON from LLM: %s", result)
	}
	if parsed.Title == "" || parsed.Summary == "" {
		return "", "", "", fmt.Errorf("missing title or summary: %s", result)
	}

	log.Printf("LLM summarized notification: %q / %q", parsed.Title, parsed.Summary)
	return parsed.Title, parsed.Summary, parsed.Details, nil
}

const notifGenPrompt = `Generate a realistic random notification. You MUST pick a DIFFERENT category each time from this list — never repeat the same type twice in a row:
- CI/CD build status for a software project (e.g. "Build #847 failed on main", "Deploy to staging complete")
- Calendar/meeting reminder (e.g. "Design review in 15 min", "1:1 with Sarah moved to 3pm")
- Git/code review (e.g. "PR #234 approved by Alex", "3 comments on your PR")
- Server/infra alert (e.g. "CPU usage at 92% on prod-west-2", "SSL cert expires in 3 days")
- Weather update (e.g. "Rain starting in 20 min", "Freeze warning tonight")
- Smart home event (e.g. "Front door unlocked", "Garage door left open 30 min")
- News/finance (e.g. "AAPL up 4.2% after earnings", "Breaking: Fed holds rates steady")
- Health/fitness (e.g. "Stand goal reached", "Heart rate elevated to 120 bpm")
- Message/social (e.g. "2 new messages in #engineering", "Mom shared a photo")

Reply with ONLY a JSON object, no other text:
{"title": "short title", "summary": "1-2 sentence summary to read aloud", "details": "optional extra context, or empty string", "priority": "low|normal|high", "source": "source app or system name"}

Be specific with names, numbers, times. Make it feel like a real notification.`

// GenerateNotification asks the LLM to produce a random notification JSON.
func (e *Engine) GenerateNotification() (map[string]string, error) {
	reqBody, _ := json.Marshal(chatRequest{
		Model: "qwen3",
		Messages: []chatMessage{
			{Role: "system", Content: notifGenPrompt},
			{Role: "user", Content: "Generate one notification. /no_think"},
		},
		MaxTokens:   256,
		Temperature: 1.0,
	})

	resp, err := http.Post(e.apiURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM error %d: %s", resp.StatusCode, string(body))
	}

	var data chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}
	if len(data.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	result := data.Choices[0].Message.Content
	if idx := strings.Index(result, "</think>"); idx >= 0 {
		result = result[idx+len("</think>"):]
	}
	result = strings.TrimSpace(result)

	var notif map[string]string
	if err := json.Unmarshal([]byte(result), &notif); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %s", result)
	}
	if notif["title"] == "" || notif["summary"] == "" {
		return nil, fmt.Errorf("missing title or summary: %s", result)
	}
	return notif, nil
}

// Close stops the llama-server subprocess.
func (e *Engine) Close() {
	if e.cmd != nil && e.cmd.Process != nil {
		log.Printf("Stopping llama-server (pid %d)", e.cmd.Process.Pid)
		e.cmd.Process.Kill()
		e.cmd.Wait()
	}
}

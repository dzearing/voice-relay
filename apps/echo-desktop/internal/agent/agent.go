package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const agentSystemPromptTemplate = `You are a helpful voice assistant. Today is %s. The user is speaking to you and your response will be read aloud. You CANNOT show the user any links, web pages, or visual content — they can only hear your words. So:
- Keep responses concise (1-3 sentences when possible)
- Use natural, conversational language
- Avoid markdown, bullet points, URLs, or formatting — just plain spoken text
- NEVER say "check the link", "see the results", or reference any visual content
- When you use search results, extract the key facts and state them directly as if you already know them
- If you don't know something and can't find it, say so briefly

You have access to tools. Use them when the user asks about current events, facts you're unsure about, or anything that benefits from a web search. Do not use tools for simple greetings or questions you can confidently answer.`

func agentSystemPrompt() string {
	return fmt.Sprintf(agentSystemPromptTemplate, time.Now().Format("Monday, January 2, 2006"))
}

const maxIterations = 3

// ProgressFunc is called by the agent to report state changes during execution.
// state is one of: "thinking", "searching"
// detail provides additional info (e.g. tool name for "searching")
type ProgressFunc func(state, detail string)

// Agent runs an LLM agent loop with tool calling.
type Agent struct {
	apiURL   string
	tools    []ToolDef
	handlers map[string]ToolHandler
	oaiTools []map[string]interface{}
}

// NewAgent creates a new Agent, loading tools from the given directory.
func NewAgent(apiURL, toolsDir string) (*Agent, error) {
	tools, err := LoadTools(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("loading tools: %w", err)
	}

	handlers := make(map[string]ToolHandler)
	for _, t := range tools {
		if h, ok := builtinHandlers[t.Type]; ok {
			handlers[t.Name] = h
		}
	}

	return &Agent{
		apiURL:   apiURL,
		tools:    tools,
		handlers: handlers,
		oaiTools: BuildOpenAITools(tools),
	}, nil
}

// chatMessage is an OpenAI-format chat message.
type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// Run executes the agent loop for a user utterance and returns the text response.
func (a *Agent) Run(userText string) (string, error) {
	return a.RunWithProgress(userText, nil)
}

// RunWithProgress executes the agent loop with a progress callback.
func (a *Agent) RunWithProgress(userText string, onProgress ProgressFunc) (string, error) {
	log.Printf("Agent.Run: input=%q, tools=%d", userText, len(a.tools))

	// First call: NO /no_think — thinking is needed for tool calling decisions
	messages := []chatMessage{
		{Role: "system", Content: agentSystemPrompt()},
		{Role: "user", Content: userText},
	}

	for iteration := 0; iteration < maxIterations; iteration++ {
		// On the last iteration, omit tools to force a text response
		var tools []map[string]interface{}
		if iteration < maxIterations-1 {
			tools = a.oaiTools
		}

		log.Printf("Agent iteration %d: sending %d messages, tools=%v", iteration, len(messages), len(tools) > 0)
		resp, err := a.callLLM(messages, tools)
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			log.Printf("Agent iteration %d: no choices returned", iteration)
			return "I'm sorry, I couldn't generate a response.", nil
		}

		choice := resp.Choices[0].Message

		// No tool calls — return the content as final answer
		if len(choice.ToolCalls) == 0 {
			log.Printf("Agent iteration %d: final text response (len=%d)", iteration, len(choice.Content))
			return a.cleanResponse(choice.Content), nil
		}

		log.Printf("Agent iteration %d: %d tool calls requested", iteration, len(choice.ToolCalls))

		// Append the assistant message with tool calls
		messages = append(messages, chatMessage{
			Role:      "assistant",
			Content:   choice.Content,
			ToolCalls: choice.ToolCalls,
		})

		// Execute each tool call
		for _, tc := range choice.ToolCalls {
			// Notify progress: searching
			if onProgress != nil {
				onProgress("searching", tc.Function.Name)
			}

			result := a.executeTool(tc)
			log.Printf("Agent tool result for %s (len=%d): %.200s", tc.Function.Name, len(result), result)
			messages = append(messages, chatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}

		// After tool results, add /no_think to speed up response generation
		// (thinking was needed for the tool decision, not for synthesizing results)
		messages = append(messages, chatMessage{
			Role:    "user",
			Content: "Now answer my original question using the information above. Be concise — this will be spoken aloud. /no_think",
		})

		// Notify progress: thinking (going back to LLM with tool results)
		if onProgress != nil {
			onProgress("thinking", "")
		}
	}

	// If we exhausted iterations, make one final call without tools
	resp, err := a.callLLM(messages, nil)
	if err != nil {
		return "", fmt.Errorf("final LLM call failed: %w", err)
	}
	if len(resp.Choices) > 0 {
		return a.cleanResponse(resp.Choices[0].Message.Content), nil
	}
	return "I'm sorry, I couldn't generate a response.", nil
}

func (a *Agent) callLLM(messages []chatMessage, tools []map[string]interface{}) (*chatResponse, error) {
	reqBody := map[string]interface{}{
		"model":       "qwen3",
		"messages":    messages,
		"max_tokens":  1024,
		"temperature": 0.7,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
		reqBody["tool_choice"] = "auto"
	}

	data, _ := json.Marshal(reqBody)
	resp, err := http.Post(a.apiURL+"/v1/chat/completions", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading LLM response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("LLM raw response: %.500s", string(body))

	var result chatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decoding LLM response: %w", err)
	}
	return &result, nil
}

func (a *Agent) executeTool(tc toolCall) string {
	handler, ok := a.handlers[tc.Function.Name]
	if !ok {
		return fmt.Sprintf("Error: unknown tool '%s'", tc.Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("Error parsing tool arguments: %v", err)
	}

	log.Printf("Agent tool call: %s(%v)", tc.Function.Name, args)
	result, err := handler(args)
	if err != nil {
		return fmt.Sprintf("Tool error: %v", err)
	}
	return result
}

// cleanResponse strips <think>...</think> blocks and trims whitespace.
func (a *Agent) cleanResponse(text string) string {
	if idx := strings.Index(text, "</think>"); idx >= 0 {
		text = text[idx+len("</think>"):]
	}
	return strings.TrimSpace(text)
}

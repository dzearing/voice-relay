package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ToolDef describes a tool loaded from YAML.
type ToolDef struct {
	Name        string                 `yaml:"name"`
	Type        string                 `yaml:"type"`
	Description string                 `yaml:"description"`
	Parameters  map[string]interface{} `yaml:"parameters"`
}

// ToolHandler executes a tool call and returns the result text.
type ToolHandler func(args map[string]interface{}) (string, error)

// builtinHandlers maps tool type strings to their Go implementations.
var builtinHandlers = map[string]ToolHandler{
	"web_search": WebSearchHandler,
}

// LoadTools reads all .yaml files from dir and returns parsed ToolDefs.
func LoadTools(dir string) ([]ToolDef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading tools dir: %w", err)
	}

	var tools []ToolDef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t ToolDef
		if err := yaml.Unmarshal(data, &t); err != nil || t.Name == "" {
			continue
		}
		tools = append(tools, t)
	}
	return tools, nil
}

// EnsureDefaultTools creates the tools directory and a default web_search.yaml if missing.
func EnsureDefaultTools(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	wsPath := filepath.Join(dir, "web_search.yaml")
	if _, err := os.Stat(wsPath); err == nil {
		return nil // already exists
	}

	defaultYAML := `name: web_search
type: web_search
description: "Search the web for current information, news, facts, or any topic."
parameters:
  type: object
  properties:
    query:
      type: string
      description: "The search query"
  required:
    - query
`
	return os.WriteFile(wsPath, []byte(defaultYAML), 0644)
}

// BuildOpenAITools converts ToolDefs into the OpenAI function calling format.
func BuildOpenAITools(defs []ToolDef) []map[string]interface{} {
	var tools []map[string]interface{}
	for _, d := range defs {
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        d.Name,
				"description": d.Description,
				"parameters":  d.Parameters,
			},
		})
	}
	return tools
}

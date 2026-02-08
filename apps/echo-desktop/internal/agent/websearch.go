package agent

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SearchResult holds a single web search result.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// WebSearchHandler is the builtin handler for the web_search tool type.
func WebSearchHandler(args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "Error: no search query provided", nil
	}

	results, err := searchDuckDuckGo(query)
	if err != nil {
		return fmt.Sprintf("Search failed: %v. I'll answer based on what I know.", err), nil
	}

	if len(results) == 0 {
		return "No search results found.", nil
	}

	// Format results as readable text for the LLM
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s\n\n", query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		sb.WriteString(fmt.Sprintf("   %s\n\n", r.URL))
	}
	return sb.String(), nil
}

// searchDuckDuckGo queries DuckDuckGo HTML Lite and parses results.
func searchDuckDuckGo(query string) ([]SearchResult, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	form := url.Values{"q": {query}}
	req, err := http.NewRequest("POST", "https://lite.duckduckgo.com/lite/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; VoiceRelay/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DuckDuckGo request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return parseDDGLite(string(body)), nil
}

// findClass finds `class="name"` or `class='name'` in the string and returns the index.
func findClass(s, name string) int {
	for _, q := range []string{"'", "\""} {
		needle := "class=" + q + name + q
		if idx := strings.Index(s, needle); idx >= 0 {
			return idx
		}
	}
	return -1
}

// parseDDGLite extracts search results from DuckDuckGo Lite HTML using string operations.
func parseDDGLite(html string) []SearchResult {
	var results []SearchResult
	const maxResults = 5

	// DDG Lite uses <a class='result-link'> for titles/URLs and
	// <td class='result-snippet'> for snippets (single-quoted attributes).
	remaining := html
	for len(results) < maxResults {
		// Find next result link
		linkIdx := findClass(remaining, "result-link")
		if linkIdx < 0 {
			break
		}

		// Extract the href from the <a> tag preceding or containing class='result-link'
		// Back up to find the <a tag start
		tagStart := strings.LastIndex(remaining[:linkIdx], "<a")
		if tagStart < 0 {
			remaining = remaining[linkIdx+19:]
			continue
		}

		tagChunk := remaining[tagStart:]
		href := extractAttr(tagChunk, "href")

		// Extract the link text (between > and </a>)
		closeBracket := strings.Index(tagChunk, ">")
		if closeBracket < 0 {
			remaining = remaining[linkIdx+19:]
			continue
		}
		endTag := strings.Index(tagChunk[closeBracket:], "</a>")
		title := ""
		if endTag >= 0 {
			title = stripTags(tagChunk[closeBracket+1 : closeBracket+endTag])
		}

		// Move past this result link
		remaining = remaining[linkIdx+19:]

		if href == "" || title == "" {
			continue
		}

		// Look for the snippet in the next chunk
		snippet := ""
		snippetIdx := findClass(remaining, "result-snippet")
		nextLinkIdx := findClass(remaining, "result-link")

		// Only grab snippet if it appears before the next result link
		if snippetIdx >= 0 && (nextLinkIdx < 0 || snippetIdx < nextLinkIdx) {
			// Find the > after the td tag
			afterSnippet := remaining[snippetIdx:]
			gt := strings.Index(afterSnippet, ">")
			if gt >= 0 {
				endTd := strings.Index(afterSnippet[gt:], "</td>")
				if endTd >= 0 {
					snippet = stripTags(afterSnippet[gt+1 : gt+endTd])
				}
			}
		}

		results = append(results, SearchResult{
			Title:   strings.TrimSpace(title),
			URL:     strings.TrimSpace(href),
			Snippet: strings.TrimSpace(snippet),
		})
	}

	return results
}

// extractAttr extracts the value of an HTML attribute from a tag string.
// Handles both single and double quoted attribute values.
func extractAttr(tag, attr string) string {
	for _, q := range []string{"\"", "'"} {
		key := attr + "=" + q
		idx := strings.Index(tag, key)
		if idx < 0 {
			continue
		}
		start := idx + len(key)
		end := strings.Index(tag[start:], q)
		if end < 0 {
			continue
		}
		return tag[start : start+end]
	}
	return ""
}

// stripTags removes HTML tags and decodes common entities.
func stripTags(s string) string {
	var out strings.Builder
	inTag := false
	for _, c := range s {
		if c == '<' {
			inTag = true
			continue
		}
		if c == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(c)
		}
	}
	result := out.String()
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", "\"")
	result = strings.ReplaceAll(result, "&#x27;", "'")
	result = strings.ReplaceAll(result, "&nbsp;", " ")
	return strings.TrimSpace(result)
}

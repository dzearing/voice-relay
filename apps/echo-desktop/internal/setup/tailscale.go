package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// TailscaleInfo holds detected Tailscale network information.
type TailscaleInfo struct {
	Available bool
	IP        string
	DNSName   string
	FunnelURL string // e.g. "https://machine.tail1234.ts.net"
	ShortURL  string // e.g. "https://tinyurl.com/abc123"
}

type tailscaleStatus struct {
	Self struct {
		TailscaleIPs []string `json:"TailscaleIPs"`
		DNSName      string   `json:"DNSName"`
	} `json:"Self"`
}

type funnelStatus struct {
	Web map[string]struct {
		Handlers map[string]struct {
			Proxy string `json:"Proxy"`
		} `json:"Handlers"`
	} `json:"Web"`
}

// DetectTailscale runs `tailscale status --json` and extracts the local Tailscale IP and DNS name.
func DetectTailscale() TailscaleInfo {
	info := TailscaleInfo{}

	cmd := exec.Command("tailscale", "status", "--json")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return info
	}

	var status tailscaleStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return info
	}

	info.Available = true
	if len(status.Self.TailscaleIPs) > 0 {
		info.IP = status.Self.TailscaleIPs[0]
	}
	info.DNSName = strings.TrimSuffix(status.Self.DNSName, ".")

	return info
}

// DetectFunnel checks if Tailscale Funnel/Serve is configured and returns the URL.
func DetectFunnel() string {
	cmd := exec.Command("tailscale", "funnel", "status", "--json")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	var fs funnelStatus
	if err := json.Unmarshal(out, &fs); err != nil {
		return ""
	}

	// Extract the hostname from the Web map keys (e.g. "machine.tail1234.ts.net:443")
	for hostPort := range fs.Web {
		host := strings.TrimSuffix(hostPort, ":443")
		return fmt.Sprintf("https://%s", host)
	}

	return ""
}

// EnsureFunnel starts Tailscale Funnel for the given port if not already running.
func EnsureFunnel(port int) (string, error) {
	// Check if already running
	if url := DetectFunnel(); url != "" {
		log.Printf("Tailscale Funnel already active: %s", url)
		return url, nil
	}

	// Start funnel in background mode
	log.Printf("Starting Tailscale Funnel on port %d...", port)
	cmd := exec.Command("tailscale", "funnel", "--bg", fmt.Sprintf("%d", port))
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to start funnel: %w: %s", err, string(out))
	}

	// Re-detect the URL
	url := DetectFunnel()
	if url == "" {
		return "", fmt.Errorf("funnel started but URL not detected")
	}

	log.Printf("Tailscale Funnel started: %s", url)
	return url, nil
}

// ShortenURL creates a short URL via TinyURL.
func ShortenURL(longURL string) string {
	apiURL := fmt.Sprintf("https://tinyurl.com/api-create.php?url=%s", longURL)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		log.Printf("Failed to shorten URL: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("TinyURL returned status %d", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	short := strings.TrimSpace(string(body))
	// Validate: must be a short tinyurl.com URL (not an error page)
	if strings.HasPrefix(short, "https://tinyurl.com/") && len(short) < 60 {
		log.Printf("Shortened URL: %s -> %s", longURL, short)
		return short
	}

	log.Printf("TinyURL returned unexpected response: %s", short[:min(len(short), 100)])
	return ""
}

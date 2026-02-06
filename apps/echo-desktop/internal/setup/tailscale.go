package setup

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// TailscaleInfo holds detected Tailscale network information.
type TailscaleInfo struct {
	Available bool
	IP        string
	DNSName   string
}

type tailscaleStatus struct {
	Self struct {
		TailscaleIPs []string `json:"TailscaleIPs"`
		DNSName      string   `json:"DNSName"`
	} `json:"Self"`
}

// DetectTailscale runs `tailscale status --json` and extracts the local Tailscale IP and DNS name.
func DetectTailscale() TailscaleInfo {
	info := TailscaleInfo{}

	out, err := exec.Command("tailscale", "status", "--json").Output()
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

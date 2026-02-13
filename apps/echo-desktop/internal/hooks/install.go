package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/voice-relay/echo-desktop/internal/config"
)

// scriptBasename returns the hook script filename for the current OS.
func scriptBasename() string {
	if runtime.GOOS == "windows" {
		return "notify-done.ps1"
	}
	return "notify-done.sh"
}

// askScriptBasename returns the ask-intercept hook script filename for the current OS.
func askScriptBasename() string {
	if runtime.GOOS == "windows" {
		return "ask-intercept.ps1"
	}
	return "ask-intercept.sh"
}

// scriptPath returns the full path to the hook script.
func scriptPath() string {
	return filepath.Join(config.Dir(), "hooks", scriptBasename())
}

// askScriptPath returns the full path to the ask-intercept hook script.
func askScriptPath() string {
	return filepath.Join(config.Dir(), "hooks", askScriptBasename())
}

// command returns the shell command that Claude Code should run for the hook.
func command(path string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`powershell -ExecutionPolicy Bypass -NoProfile -File "%s"`, path)
	}
	return fmt.Sprintf(`bash "%s"`, path)
}

// claudeSettingsPath returns the path to ~/.claude/settings.json.
func claudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// Install writes the hook scripts and merges Stop + PreToolUse hook entries into ~/.claude/settings.json.
func Install(notifDir string) error {
	sp := scriptPath()
	asp := askScriptPath()

	// Write hook scripts
	if err := os.MkdirAll(filepath.Dir(sp), 0755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}

	var script, askScript string
	if runtime.GOOS == "windows" {
		script = windowsScript(notifDir)
		askScript = windowsAskScript()
	} else {
		script = unixScript(notifDir)
		askScript = unixAskScript()
	}

	if err := os.WriteFile(sp, []byte(script), 0755); err != nil {
		return fmt.Errorf("write hook script: %w", err)
	}

	if err := os.WriteFile(asp, []byte(askScript), 0755); err != nil {
		return fmt.Errorf("write ask-intercept script: %w", err)
	}

	// Merge into ~/.claude/settings.json
	settingsPath := claudeSettingsPath()
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}

	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}

	cmd := command(sp)
	mergeStopHook(settings, cmd)

	askCmd := command(asp)
	mergePreToolUseHook(settings, askCmd)

	return writeSettings(settingsPath, settings)
}

// Uninstall removes VoiceRelay hooks from ~/.claude/settings.json and deletes the scripts.
func Uninstall() error {
	settingsPath := claudeSettingsPath()
	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}

	removeStopHook(settings)
	removePreToolUseHook(settings)

	if err := writeSettings(settingsPath, settings); err != nil {
		return err
	}

	// Delete hook scripts
	os.Remove(scriptPath())
	os.Remove(askScriptPath())

	return nil
}

// Status checks whether all VoiceRelay hooks (Stop + PreToolUse) are installed.
func Status() (installed bool, path string) {
	settingsPath := claudeSettingsPath()
	settings, err := readSettings(settingsPath)
	if err != nil {
		return false, ""
	}

	hooksRaw, ok := settings["hooks"]
	if !ok {
		return false, ""
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		return false, ""
	}

	// Check Stop hook
	sp := scriptPath()
	stopCmd := command(sp)
	if !hookEntryExists(hooksMap, "Stop", stopCmd) {
		return false, ""
	}

	// Check PreToolUse hook
	asp := askScriptPath()
	askCmd := command(asp)
	if !hookEntryExists(hooksMap, "PreToolUse", askCmd) {
		return false, ""
	}

	return true, sp
}

// hookEntryExists checks whether a hook entry with the given command exists
// under the specified hook type (e.g. "Stop", "PreToolUse").
func hookEntryExists(hooksMap map[string]interface{}, hookType, cmd string) bool {
	listRaw, ok := hooksMap[hookType]
	if !ok {
		return false
	}
	listArr, ok := listRaw.([]interface{})
	if !ok {
		return false
	}
	for _, entry := range listArr {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		innerHooks, ok := entryMap["hooks"]
		if !ok {
			continue
		}
		innerArr, ok := innerHooks.([]interface{})
		if !ok {
			continue
		}
		for _, h := range innerArr {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if c, ok := hMap["command"].(string); ok && c == cmd {
				return true
			}
		}
	}
	return false
}

// readSettings reads and parses ~/.claude/settings.json, returning an empty map if missing.
func readSettings(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, fmt.Errorf("read settings: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse settings: %w", err)
	}
	return settings, nil
}

// writeSettings atomically writes settings.json.
func writeSettings(path string, settings map[string]interface{}) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	data = append(data, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename settings: %w", err)
	}
	return nil
}

// mergeStopHook adds a VoiceRelay Stop hook entry if not already present.
func mergeStopHook(settings map[string]interface{}, cmd string) {
	// Ensure hooks map
	hooksRaw, ok := settings["hooks"]
	if !ok {
		hooksRaw = map[string]interface{}{}
		settings["hooks"] = hooksRaw
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		hooksMap = map[string]interface{}{}
		settings["hooks"] = hooksMap
	}

	// Build the hook entry
	hookEntry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": cmd,
				"timeout": 15,
			},
		},
	}

	// Check if already present
	stopRaw, ok := hooksMap["Stop"]
	if !ok {
		hooksMap["Stop"] = []interface{}{hookEntry}
		return
	}
	stopArr, ok := stopRaw.([]interface{})
	if !ok {
		hooksMap["Stop"] = []interface{}{hookEntry}
		return
	}

	// Check for existing VoiceRelay entry
	for _, entry := range stopArr {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		innerHooks, ok := entryMap["hooks"]
		if !ok {
			continue
		}
		innerArr, ok := innerHooks.([]interface{})
		if !ok {
			continue
		}
		for _, h := range innerArr {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if c, ok := hMap["command"].(string); ok && c == cmd {
				return // already installed
			}
		}
	}

	hooksMap["Stop"] = append(stopArr, hookEntry)
}

// removeStopHook removes VoiceRelay Stop hook entries (matching our script path).
func removeStopHook(settings map[string]interface{}) {
	hooksRaw, ok := settings["hooks"]
	if !ok {
		return
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		return
	}
	stopRaw, ok := hooksMap["Stop"]
	if !ok {
		return
	}
	stopArr, ok := stopRaw.([]interface{})
	if !ok {
		return
	}

	sp := scriptPath()
	var filtered []interface{}
	for _, entry := range stopArr {
		if matchesScript(entry, sp) {
			continue
		}
		filtered = append(filtered, entry)
	}

	if len(filtered) == 0 {
		delete(hooksMap, "Stop")
		// Clean up empty hooks map
		if len(hooksMap) == 0 {
			delete(settings, "hooks")
		}
	} else {
		hooksMap["Stop"] = filtered
	}
}

// mergePreToolUseHook adds a VoiceRelay PreToolUse hook entry for AskUserQuestion if not already present.
func mergePreToolUseHook(settings map[string]interface{}, cmd string) {
	hooksRaw, ok := settings["hooks"]
	if !ok {
		hooksRaw = map[string]interface{}{}
		settings["hooks"] = hooksRaw
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		hooksMap = map[string]interface{}{}
		settings["hooks"] = hooksMap
	}

	hookEntry := map[string]interface{}{
		"matcher": "AskUserQuestion",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": cmd,
				"timeout": 10,
			},
		},
	}

	preRaw, ok := hooksMap["PreToolUse"]
	if !ok {
		hooksMap["PreToolUse"] = []interface{}{hookEntry}
		return
	}
	preArr, ok := preRaw.([]interface{})
	if !ok {
		hooksMap["PreToolUse"] = []interface{}{hookEntry}
		return
	}

	// Check if already present
	for _, entry := range preArr {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		innerHooks, ok := entryMap["hooks"]
		if !ok {
			continue
		}
		innerArr, ok := innerHooks.([]interface{})
		if !ok {
			continue
		}
		for _, h := range innerArr {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if c, ok := hMap["command"].(string); ok && c == cmd {
				return // already installed
			}
		}
	}

	hooksMap["PreToolUse"] = append(preArr, hookEntry)
}

// removePreToolUseHook removes VoiceRelay PreToolUse hook entries.
func removePreToolUseHook(settings map[string]interface{}) {
	hooksRaw, ok := settings["hooks"]
	if !ok {
		return
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		return
	}
	preRaw, ok := hooksMap["PreToolUse"]
	if !ok {
		return
	}
	preArr, ok := preRaw.([]interface{})
	if !ok {
		return
	}

	asp := askScriptPath()
	var filtered []interface{}
	for _, entry := range preArr {
		if matchesScript(entry, asp) {
			continue
		}
		filtered = append(filtered, entry)
	}

	if len(filtered) == 0 {
		delete(hooksMap, "PreToolUse")
		if len(hooksMap) == 0 {
			delete(settings, "hooks")
		}
	} else {
		hooksMap["PreToolUse"] = filtered
	}
}

// matchesScript checks if a Stop hook entry references our script path.
func matchesScript(entry interface{}, sp string) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	innerHooks, ok := entryMap["hooks"]
	if !ok {
		return false
	}
	innerArr, ok := innerHooks.([]interface{})
	if !ok {
		return false
	}
	for _, h := range innerArr {
		hMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if c, ok := hMap["command"].(string); ok && strings.Contains(c, sp) {
			return true
		}
	}
	return false
}

// windowsScript returns the PowerShell hook script with the notification directory baked in.
func windowsScript(notifDir string) string {
	// Escape backslashes for embedding in the script
	return `# notify-done.ps1 — Claude Code "Stop" hook (auto-installed by VoiceRelay)
$ErrorActionPreference = "Stop"
$notifDir = "` + notifDir + `"
$logFile = Join-Path $notifDir "hook-debug.log"

function Log($msg) {
    $ts = Get-Date -Format "HH:mm:ss"
    Add-Content -Path $logFile -Value "[$ts] $msg" -Encoding UTF8
}

try {

Log "Hook invoked."

# --- Read hook input from stdin ---
$raw = ""
try { $raw = [Console]::In.ReadToEnd() } catch {}
if (-not $raw) {
    $raw = @($input) -join "` + "`" + `n"
}
if (-not $raw) { Log "No stdin received, exiting."; exit 0 }
$hookData = $raw | ConvertFrom-Json

if ($hookData.stop_hook_active -eq $true) { Log "stop_hook_active=true, skipping."; exit 0 }

$transcriptPath = $hookData.transcript_path
if (-not $transcriptPath -or -not (Test-Path $transcriptPath)) { Log "Transcript not found, exiting."; exit 0 }

# --- Parse the last portion of the transcript JSONL ---
$lines = Get-Content $transcriptPath -Tail 200 -Encoding UTF8

$lastUserText = ""
$assistantTexts = @()

foreach ($ln in $lines) {
    if (-not $ln.Trim()) { continue }
    try { $msg = $ln | ConvertFrom-Json } catch { continue }

    $entryType = $msg.type
    if (-not $entryType) { continue }
    $inner = $msg.message
    if (-not $inner) { continue }

    if ($entryType -eq "user") {
        $c = $inner.content
        $extractUserText = {
            param($text)
            if (-not $text) { return "" }
            if ($text -match '<tool_result|<tool_use') { return "" }
            $cleaned = $text -replace '(?s)<system-reminder>.*?</system-reminder>', '' ` + "`" + `
                              -replace '(?s)<local-command-caveat>.*?</local-command-caveat>', '' ` + "`" + `
                              -replace '(?s)<command-name>.*?</command-name>', '' ` + "`" + `
                              -replace '(?s)<command-message>.*?</command-message>', '' ` + "`" + `
                              -replace '(?s)<command-args>.*?</command-args>', '' ` + "`" + `
                              -replace '(?s)<local-command-stdout>.*?</local-command-stdout>', ''
            return $cleaned.Trim()
        }
        if ($c -is [string]) {
            $cleaned = & $extractUserText $c
            if ($cleaned.Length -gt 0 -and $cleaned.Length -lt 2000) {
                $lastUserText = $cleaned
            }
        } elseif ($c -is [array]) {
            foreach ($b in $c) {
                if ($b.type -eq "text") {
                    $cleaned = & $extractUserText $b.text
                    if ($cleaned.Length -gt 0 -and $cleaned.Length -lt 2000) {
                        $lastUserText = $cleaned
                    }
                }
            }
        }
        $assistantTexts = @()
    }

    if ($entryType -eq "assistant") {
        $c = $inner.content
        if ($c -is [string] -and $c) {
            $assistantTexts += $c
        } elseif ($c -is [array]) {
            foreach ($b in $c) {
                if ($b.type -eq "text" -and $b.text) {
                    $assistantTexts += $b.text
                }
            }
        }
    }
}

$rawAssistant = ($assistantTexts -join "` + "`" + `n").Trim()
if (-not $rawAssistant) { Log "No assistant text found, exiting."; exit 0 }
if (-not $lastUserText) { $lastUserText = "(no user text captured)" }

# Truncate to keep JSON reasonable
if ($rawAssistant.Length -gt 4000) { $rawAssistant = $rawAssistant.Substring(0, 4000) }
if ($lastUserText.Length -gt 1000) { $lastUserText = $lastUserText.Substring(0, 1000) }

# --- Write notification JSON ---
$ts = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$id = "claude-$ts"

$repo = ""; $branch = ""
try {
    $repo = (git rev-parse --show-toplevel 2>$null | Split-Path -Leaf)
    $branch = (git branch --show-current 2>$null)
} catch {}

$notifObj = @{
    id                 = $id
    title              = ""
    summary            = ""
    raw_user_text      = $lastUserText
    raw_assistant_text = $rawAssistant
    priority           = "normal"
    source             = "claude-code"
    created_at         = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
}
if ($repo) { $notifObj["repo"] = $repo }
if ($branch) { $notifObj["branch"] = $branch }
if ($env:CC_SESSION) { $notifObj["session"] = $env:CC_SESSION }
if ($env:CC_WRAPPER_NAME) { $notifObj["reply_target"] = $env:CC_WRAPPER_NAME }
$notif = $notifObj | ConvertTo-Json -Compress

$pendingDir = Join-Path $notifDir "pending"
$tmpDir = Join-Path $notifDir "tmp"
foreach ($d in @($pendingDir, $tmpDir)) {
    if (-not (Test-Path $d)) { New-Item -ItemType Directory -Path $d -Force | Out-Null }
}

$tmpFile = Join-Path $tmpDir "$id.json"
$finalFile = Join-Path $pendingDir "$id.json"

$utf8 = New-Object System.Text.UTF8Encoding($false)
[IO.File]::WriteAllText($tmpFile, $notif, $utf8)
Move-Item -Path $tmpFile -Destination $finalFile -Force

Log "Wrote raw notification: $id (user: $($lastUserText.Length) chars, assistant: $($rawAssistant.Length) chars)"

} catch {
    Log "ERROR: $_"
}
exit 0
`
}

// unixScript returns the Bash hook script with the notification directory baked in.
func unixScript(notifDir string) string {
	return `#!/usr/bin/env bash
# notify-done.sh — Claude Code "Stop" hook (auto-installed by VoiceRelay)
set -euo pipefail

NOTIF_DIR="` + notifDir + `"
LOG_FILE="$NOTIF_DIR/hook-debug.log"

log() { echo "[$(date +%H:%M:%S)] $*" >> "$LOG_FILE"; }

log "Hook invoked."

# Read hook input from stdin
RAW=$(cat)
if [ -z "$RAW" ]; then log "No stdin received, exiting."; exit 0; fi

# Parse with python3 (available on macOS and most Linux with Claude Code)
RESULT=$(python3 -c "
import json, sys, re, os

data = json.loads(sys.argv[1])
if data.get('stop_hook_active'):
    print('SKIP')
    sys.exit(0)

tp = data.get('transcript_path', '')
if not tp or not os.path.exists(tp):
    print('NO_TRANSCRIPT')
    sys.exit(0)

with open(tp, 'r', encoding='utf-8') as f:
    lines = f.readlines()[-200:]

last_user = ''
assistant_texts = []

tag_re = re.compile(r'<(?:system-reminder|local-command-caveat|command-name|command-message|command-args|local-command-stdout)>.*?</(?:system-reminder|local-command-caveat|command-name|command-message|command-args|local-command-stdout)>', re.DOTALL)

for ln in lines:
    ln = ln.strip()
    if not ln:
        continue
    try:
        msg = json.loads(ln)
    except:
        continue
    t = msg.get('type', '')
    inner = msg.get('message', {})
    if not inner:
        continue
    c = inner.get('content', '')

    if t == 'user':
        def extract(text):
            if not text:
                return ''
            if '<tool_result' in text or '<tool_use' in text:
                return ''
            return tag_re.sub('', text).strip()

        if isinstance(c, str):
            cleaned = extract(c)
            if 0 < len(cleaned) < 2000:
                last_user = cleaned
        elif isinstance(c, list):
            for b in c:
                if b.get('type') == 'text':
                    cleaned = extract(b.get('text', ''))
                    if 0 < len(cleaned) < 2000:
                        last_user = cleaned
        assistant_texts = []

    if t == 'assistant':
        if isinstance(c, str) and c:
            assistant_texts.append(c)
        elif isinstance(c, list):
            for b in c:
                if b.get('type') == 'text' and b.get('text'):
                    assistant_texts.append(b['text'])

raw_assistant = '\n'.join(assistant_texts).strip()
if not raw_assistant:
    print('NO_ASSISTANT')
    sys.exit(0)

if not last_user:
    last_user = '(no user text captured)'

raw_assistant = raw_assistant[:4000]
last_user = last_user[:1000]

import time
ts = int(time.time() * 1000)
nid = f'claude-{ts}'
from datetime import datetime, timezone
now = datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')

import subprocess
repo = ''
branch = ''
try:
    repo = os.path.basename(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], stderr=subprocess.DEVNULL).decode().strip())
    branch = subprocess.check_output(['git', 'branch', '--show-current'], stderr=subprocess.DEVNULL).decode().strip()
except Exception:
    pass

notif_obj = {
    'id': nid,
    'title': '',
    'summary': '',
    'raw_user_text': last_user,
    'raw_assistant_text': raw_assistant,
    'priority': 'normal',
    'source': 'claude-code',
    'created_at': now,
}
if repo:
    notif_obj['repo'] = repo
if branch:
    notif_obj['branch'] = branch
cc_session = os.environ.get('CC_SESSION')
if cc_session:
    notif_obj['session'] = cc_session
cc_wrapper = os.environ.get('CC_WRAPPER_NAME')
if cc_wrapper:
    notif_obj['reply_target'] = cc_wrapper
notif = json.dumps(notif_obj, ensure_ascii=False)

print('OK')
print(nid)
print(notif)
" "$RAW" 2>>"$LOG_FILE")

STATUS=$(echo "$RESULT" | head -1)

case "$STATUS" in
    SKIP) log "stop_hook_active=true, skipping."; exit 0 ;;
    NO_TRANSCRIPT) log "Transcript not found, exiting."; exit 0 ;;
    NO_ASSISTANT) log "No assistant text found, exiting."; exit 0 ;;
    OK) ;;
    *) log "Unexpected status: $STATUS"; exit 0 ;;
esac

NOTIF_ID=$(echo "$RESULT" | sed -n '2p')
NOTIF_JSON=$(echo "$RESULT" | tail -n +3)

PENDING_DIR="$NOTIF_DIR/pending"
TMP_DIR="$NOTIF_DIR/tmp"
mkdir -p "$PENDING_DIR" "$TMP_DIR"

TMP_FILE="$TMP_DIR/$NOTIF_ID.json"
FINAL_FILE="$PENDING_DIR/$NOTIF_ID.json"

echo -n "$NOTIF_JSON" > "$TMP_FILE"
mv "$TMP_FILE" "$FINAL_FILE"

log "Wrote raw notification: $NOTIF_ID"
exit 0
`
}

// windowsAskScript returns the PowerShell hook script that intercepts AskUserQuestion
// tool calls and POSTs the question data to the coordinator.
func windowsAskScript() string {
	return `# ask-intercept.ps1 — Claude Code "PreToolUse" hook for AskUserQuestion (auto-installed by VoiceRelay)
$ErrorActionPreference = "SilentlyContinue"

# Read hook input from stdin
$raw = ""
try { $raw = [Console]::In.ReadToEnd() } catch {}
if (-not $raw) {
    $raw = @($input) -join "` + "`" + `n"
}
if (-not $raw) { exit 0 }

try {
    $hookData = $raw | ConvertFrom-Json
} catch { exit 0 }

$toolName = $hookData.tool_name
if ($toolName -ne "AskUserQuestion") { exit 0 }

$toolInput = $hookData.tool_input
if (-not $toolInput) { exit 0 }

# Build the question payload
$ts = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$id = "ask-$ts"

$payload = @{
    id           = $id
    reply_target = if ($env:CC_WRAPPER_NAME) { $env:CC_WRAPPER_NAME } else { "" }
    questions    = $toolInput.questions
} | ConvertTo-Json -Depth 10 -Compress

# POST to coordinator (fire-and-forget, don't block Claude)
try {
    $uri = "http://localhost:53937/hooks/question"
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($payload)
    $req = [System.Net.HttpWebRequest]::Create($uri)
    $req.Method = "POST"
    $req.ContentType = "application/json"
    $req.ContentLength = $bytes.Length
    $req.Timeout = 3000
    $stream = $req.GetRequestStream()
    $stream.Write($bytes, 0, $bytes.Length)
    $stream.Close()
    $resp = $req.GetResponse()
    $resp.Close()
} catch {}

exit 0
`
}

// unixAskScript returns the Bash hook script that intercepts AskUserQuestion
// tool calls and POSTs the question data to the coordinator.
func unixAskScript() string {
	return `#!/usr/bin/env bash
# ask-intercept.sh — Claude Code "PreToolUse" hook for AskUserQuestion (auto-installed by VoiceRelay)
set -euo pipefail

RAW=$(cat)
if [ -z "$RAW" ]; then exit 0; fi

# Use python3 to parse and POST
python3 -c "
import json, sys, os, urllib.request, time

data = json.loads(sys.argv[1])
if data.get('tool_name') != 'AskUserQuestion':
    sys.exit(0)

tool_input = data.get('tool_input', {})
questions = tool_input.get('questions', [])
if not questions:
    sys.exit(0)

ts = int(time.time() * 1000)
payload = {
    'id': f'ask-{ts}',
    'reply_target': os.environ.get('CC_WRAPPER_NAME', ''),
    'questions': questions,
}

body = json.dumps(payload).encode()
req = urllib.request.Request(
    'http://localhost:53937/hooks/question',
    data=body,
    headers={'Content-Type': 'application/json'},
    method='POST',
)
try:
    urllib.request.urlopen(req, timeout=3)
except Exception:
    pass
" "$RAW" 2>/dev/null || true

exit 0
`
}

# notify-done.ps1 — Claude Code "Stop" hook
# Reads the session transcript and drops a notification JSON with raw text
# into VoiceRelay's pending folder. The Go backend summarizes via Qwen.

$ErrorActionPreference = "Stop"
$logFile = Join-Path $env:APPDATA "VoiceRelay\notifications\hook-debug.log"

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
    $raw = @($input) -join "`n"
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
            $cleaned = $text -replace '(?s)<system-reminder>.*?</system-reminder>', '' `
                              -replace '(?s)<local-command-caveat>.*?</local-command-caveat>', '' `
                              -replace '(?s)<command-name>.*?</command-name>', '' `
                              -replace '(?s)<command-message>.*?</command-message>', '' `
                              -replace '(?s)<command-args>.*?</command-args>', '' `
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

$rawAssistant = ($assistantTexts -join "`n").Trim()
if (-not $rawAssistant) { Log "No assistant text found, exiting."; exit 0 }
if (-not $lastUserText) { $lastUserText = "(no user text captured)" }

# Truncate to keep JSON reasonable
if ($rawAssistant.Length -gt 4000) { $rawAssistant = $rawAssistant.Substring(0, 4000) }
if ($lastUserText.Length -gt 1000) { $lastUserText = $lastUserText.Substring(0, 1000) }

# --- Write notification JSON with raw fields (Go backend will summarize via Qwen) ---
$ts = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$id = "claude-$ts"

$notif = @{
    id                 = $id
    title              = ""
    summary            = ""
    raw_user_text      = $lastUserText
    raw_assistant_text = $rawAssistant
    priority           = "normal"
    source             = "claude-code"
    created_at         = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
} | ConvertTo-Json -Compress

$notifDir = Join-Path $env:APPDATA "VoiceRelay\notifications"
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

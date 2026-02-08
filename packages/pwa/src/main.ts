// Elements
const machineSelect = document.getElementById("machine") as HTMLSelectElement;
const mainBtn = document.getElementById("main-btn") as HTMLButtonElement;
const cancelBtn = document.getElementById("cancel-btn") as HTMLButtonElement;
const statusEl = document.getElementById("status") as HTMLDivElement;
const statusText = statusEl.querySelector(".status-text") as HTMLSpanElement;
const viewResultsBtn = document.getElementById("view-results-btn") as HTMLButtonElement;
const resultsOverlay = document.getElementById("results-overlay") as HTMLDivElement;
const closeOverlayBtn = document.getElementById("close-overlay-btn") as HTMLButtonElement;
const cleanedTextEl = document.getElementById("cleaned-text") as HTMLDivElement;
const rawTextEl = document.getElementById("raw-text") as HTMLDivElement;
const resendCleanedBtn = document.getElementById("resend-cleaned-btn") as HTMLButtonElement;
const resendRawBtn = document.getElementById("resend-raw-btn") as HTMLButtonElement;
const timingInfoEl = document.getElementById("timing-info") as HTMLDivElement;
const settingsBtn = document.getElementById("settings-btn") as HTMLButtonElement;
const settingsOverlay = document.getElementById("settings-overlay") as HTMLDivElement;
const closeSettingsBtn = document.getElementById("close-settings-btn") as HTMLButtonElement;
const voiceListEl = document.getElementById("voice-list") as HTMLDivElement;
const resultHeaderPrimary = document.getElementById("result-header-primary") as HTMLDivElement;
const resultHeaderSecondary = document.getElementById("result-header-secondary") as HTMLDivElement;

const transcriptionTextEl = document.getElementById("transcription-text") as HTMLDivElement;
const thinkingIndicator = document.getElementById("thinking-indicator") as HTMLDivElement;

// Action card elements
const actionCard = document.getElementById("action-card") as HTMLDivElement;
const actionSummary = document.getElementById("action-summary") as HTMLDivElement;
const actionSummaryText = document.getElementById("action-summary-text") as HTMLSpanElement;
const actionExpanded = document.getElementById("action-expanded") as HTMLDivElement;
const modeRelayBtn = document.getElementById("mode-relay-btn") as HTMLButtonElement;
const modeTalkBtn = document.getElementById("mode-talk-btn") as HTMLButtonElement;
const machinePicker = document.getElementById("machine-picker") as HTMLDivElement;

// State
type AppState = "idle" | "recording" | "sending";
type AppMode = "relay" | "talk";
let currentState: AppState = "idle";
let currentMode: AppMode = (localStorage.getItem("voicerelay_mode") as AppMode) || "relay";
let mediaStream: MediaStream | null = null;
let recorderNode: ScriptProcessorNode | null = null;
let recordedSamples: Float32Array[] = [];
let audioBlob: Blob | null = null;
let recordingStartTime: number = 0;
let recordingTimer: ReturnType<typeof setInterval> | null = null;

// Single shared AudioContext for recording AND playback.
// iOS Safari only allows one active AudioContext — using two kills the first.
let sharedCtx: AudioContext | null = null;

function ensureAudioContext(): AudioContext {
  if (!sharedCtx || sharedCtx.state === "closed") {
    sharedCtx = new AudioContext();
  }
  if (sharedCtx.state === "suspended") {
    sharedCtx.resume();
  }
  return sharedCtx;
}

// Called during user gesture to unlock audio on iOS
function primeAudio() {
  ensureAudioContext();
}

// Unique session ID for scoping agent_status events to this window
const SESSION_ID = crypto.randomUUID();

// Get API base URL
const API_BASE = window.location.origin;

// State management
function setState(state: AppState) {
  currentState = state;
  mainBtn.dataset.state = state;

  // Show/hide cancel button
  cancelBtn.classList.toggle("visible", state === "recording");

  // Enable/disable main button
  mainBtn.disabled = state === "sending";
}

// Status helpers
function setStatus(message: string, type: "normal" | "error" | "success" | "recording" | "sending" = "normal") {
  statusText.textContent = message;
  statusEl.className = "status visible";
  if (type !== "normal") {
    statusEl.classList.add(type);
  }
}

function hideStatus() {
  statusEl.classList.remove("visible");
}

// Transcription text helpers (talk mode only)
function showTranscription(text: string) {
  transcriptionTextEl.textContent = `"${text}"`;
  transcriptionTextEl.classList.add("visible");
}

function hideTranscription() {
  transcriptionTextEl.classList.remove("visible");
  thinkingIndicator.classList.remove("visible");
}

function showThinking() {
  thinkingIndicator.classList.add("visible");
}

function hideThinking() {
  thinkingIndicator.classList.remove("visible");
}

// Store current results for resending
let lastRawText = "";
let lastCleanedText = "";

function formatMs(ms: number): string {
  return (ms / 1000).toFixed(1) + "s";
}

function showResultsButton(rawText: string, cleanedText: string, sttMs?: number, llmMs?: number, totalMs?: number) {
  lastRawText = rawText;
  lastCleanedText = cleanedText;
  cleanedTextEl.textContent = cleanedText;
  rawTextEl.textContent = rawText;
  viewResultsBtn.classList.add("visible");

  // Build timing string
  const parts: string[] = [];
  if (sttMs != null && sttMs > 0) parts.push(`STT ${formatMs(sttMs)}`);
  if (llmMs != null && llmMs > 0) parts.push(`LLM ${formatMs(llmMs)}`);
  if (totalMs != null && totalMs > 0) parts.push(`Total ${formatMs(totalMs)}`);
  timingInfoEl.textContent = parts.join(" \u00B7 ");
}

interface TimingEntry {
  label: string;
  ms: number;
}

function showTalkResults(rawText: string, agentResponse: string, sttMs?: number, agentMs?: number, totalMs?: number, timings?: TimingEntry[]) {
  lastRawText = rawText;
  lastCleanedText = agentResponse;

  // Update headers for talk mode
  resultHeaderPrimary.textContent = "Response";
  resultHeaderPrimary.className = "result-header sent";
  resultHeaderSecondary.textContent = "You said";
  resultHeaderSecondary.className = "result-header raw";

  cleanedTextEl.textContent = agentResponse;
  rawTextEl.textContent = rawText;

  // Hide resend buttons in talk mode
  resendCleanedBtn.style.display = "none";
  resendRawBtn.style.display = "none";

  viewResultsBtn.classList.add("visible");

  // Build detailed timing string
  const parts: string[] = [];
  if (sttMs != null && sttMs > 0) parts.push(`STT ${formatMs(sttMs)}`);
  if (timings && timings.length > 0) {
    for (const t of timings) {
      parts.push(`${t.label} ${formatMs(t.ms)}`);
    }
  } else if (agentMs != null && agentMs > 0) {
    parts.push(`Agent ${formatMs(agentMs)}`);
  }
  if (totalMs != null && totalMs > 0) parts.push(`Total ${formatMs(totalMs)}`);
  timingInfoEl.textContent = parts.join(" \u00B7 ");
}

function showRelayResults(rawText: string, cleanedText: string, sttMs?: number, llmMs?: number, totalMs?: number) {
  // Restore headers for relay mode
  resultHeaderPrimary.textContent = "Sent (Cleaned)";
  resultHeaderPrimary.className = "result-header sent";
  resultHeaderSecondary.textContent = "Raw";
  resultHeaderSecondary.className = "result-header raw";

  // Show resend buttons in relay mode
  resendCleanedBtn.style.display = "";
  resendRawBtn.style.display = "";

  showResultsButton(rawText, cleanedText, sttMs, llmMs, totalMs);
}

function hideResultsButton() {
  viewResultsBtn.classList.remove("visible");
}

function openOverlay() {
  resultsOverlay.classList.add("visible");
  document.body.style.overflow = "hidden";
}

function closeOverlay() {
  resultsOverlay.classList.remove("visible");
  document.body.style.overflow = "";
}

// Format time as MM:SS
function formatTime(seconds: number): string {
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${mins.toString().padStart(2, "0")}:${secs.toString().padStart(2, "0")}`;
}

// Update recording time display
function updateRecordingTime() {
  const elapsed = (Date.now() - recordingStartTime) / 1000;
  setStatus(formatTime(elapsed), "recording");
}

// Persist selected device
const STORAGE_KEY = "voicerelay_target";

function saveTarget(name: string) {
  if (name) localStorage.setItem(STORAGE_KEY, name);
}

function loadTarget(): string {
  return localStorage.getItem(STORAGE_KEY) || "";
}

// --- Mode & Action Card ---

function setMode(mode: AppMode) {
  currentMode = mode;
  localStorage.setItem("voicerelay_mode", mode);

  // Update mode buttons
  modeRelayBtn.classList.toggle("active", mode === "relay");
  modeTalkBtn.classList.toggle("active", mode === "talk");

  // Show/hide machine picker
  machinePicker.classList.toggle("hidden", mode === "talk");

  // Update summary text
  updateActionSummary();

  // Collapse card after selection
  actionCard.classList.remove("expanded");
}

function updateActionSummary() {
  if (currentMode === "talk") {
    actionSummaryText.textContent = "Talking with Agent";
  } else {
    const target = machineSelect.value || machineSelect.options[0]?.text || "...";
    actionSummaryText.textContent = `Relaying to ${target}`;
  }
}

function toggleActionCard() {
  actionCard.classList.toggle("expanded");
}

// Action card events
actionSummary.addEventListener("click", toggleActionCard);
modeRelayBtn.addEventListener("click", () => setMode("relay"));
modeTalkBtn.addEventListener("click", () => setMode("talk"));

// Update machine select from a list of machines
function updateMachineList(machines: { name: string }[]) {
  if (machines.length === 0) {
    machineSelect.innerHTML = '<option value="">No devices connected</option>';
    updateActionSummary();
    return;
  }

  machineSelect.innerHTML = machines
    .map((m) => `<option value="${m.name}">${m.name}</option>`)
    .join("");

  // Auto-select if only 1 machine
  if (machines.length === 1) {
    machineSelect.value = machines[0].name;
    saveTarget(machines[0].name);
  } else {
    // Restore saved selection
    const saved = loadTarget();
    if (saved && Array.from(machineSelect.options).some(o => o.value === saved)) {
      machineSelect.value = saved;
    }
  }

  updateActionSummary();
}

// WebSocket observer for live machine list updates
let observerWs: WebSocket | null = null;

function connectObserver() {
  const wsProtocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const wsUrl = `${wsProtocol}//${window.location.host}/ws`;

  observerWs = new WebSocket(wsUrl);

  observerWs.onopen = () => {
    observerWs!.send(JSON.stringify({ type: "observe", sessionId: SESSION_ID }));
  };

  observerWs.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      if (msg.type === "machines") {
        updateMachineList(msg.machines || []);
      } else if (msg.type === "agent_status") {
        handleAgentStatus(msg);
      }
    } catch {}
  };

  observerWs.onclose = () => {
    // Reconnect after a delay
    setTimeout(connectObserver, 3000);
  };

  observerWs.onerror = () => {
    observerWs?.close();
  };
}

// Save selection when changed
machineSelect.addEventListener("change", () => {
  saveTarget(machineSelect.value);
  updateActionSummary();
});

// Check if recorded samples contain actual audio (not just silence)
function hasAudio(samples: Float32Array[]): boolean {
  let sumSquared = 0;
  let count = 0;
  for (const chunk of samples) {
    for (let i = 0; i < chunk.length; i++) {
      sumSquared += chunk[i] * chunk[i];
      count++;
    }
  }
  if (count === 0) return false;
  const rms = Math.sqrt(sumSquared / count);
  return rms > 0.005;
}

// Create WAV blob from PCM samples
function createWavBlob(samples: Float32Array[], sampleRate: number): Blob {
  // Calculate total length
  let totalLength = 0;
  for (const chunk of samples) {
    totalLength += chunk.length;
  }

  // Merge into single buffer
  const merged = new Float32Array(totalLength);
  let offset = 0;
  for (const chunk of samples) {
    merged.set(chunk, offset);
    offset += chunk.length;
  }

  // Convert to 16-bit PCM
  const pcm = new Int16Array(merged.length);
  for (let i = 0; i < merged.length; i++) {
    const s = Math.max(-1, Math.min(1, merged[i]));
    pcm[i] = s < 0 ? s * 0x8000 : s * 0x7fff;
  }

  // Build WAV header
  const wavHeader = new ArrayBuffer(44);
  const view = new DataView(wavHeader);
  const dataSize = pcm.length * 2;

  // "RIFF"
  view.setUint32(0, 0x52494646, false);
  view.setUint32(4, 36 + dataSize, true);
  // "WAVE"
  view.setUint32(8, 0x57415645, false);
  // "fmt "
  view.setUint32(12, 0x666d7420, false);
  view.setUint32(16, 16, true); // chunk size
  view.setUint16(20, 1, true); // PCM
  view.setUint16(22, 1, true); // mono
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * 2, true); // byte rate
  view.setUint16(32, 2, true); // block align
  view.setUint16(34, 16, true); // bits per sample
  // "data"
  view.setUint32(36, 0x64617461, false);
  view.setUint32(40, dataSize, true);

  return new Blob([wavHeader, pcm.buffer], { type: "audio/wav" });
}

// Start recording using the shared AudioContext
async function startRecording() {
  try {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { channelCount: 1, echoCancellation: true },
    });

    mediaStream = stream;
    const ctx = ensureAudioContext();
    const source = ctx.createMediaStreamSource(stream);

    // Use ScriptProcessorNode (widely supported) to capture raw PCM
    const bufferSize = 4096;
    recorderNode = ctx.createScriptProcessor(bufferSize, 1, 1);
    recordedSamples = [];

    recorderNode.onaudioprocess = (e) => {
      const input = e.inputBuffer.getChannelData(0);
      recordedSamples.push(new Float32Array(input));
    };

    source.connect(recorderNode);
    recorderNode.connect(ctx.destination);

    recordingStartTime = Date.now();
    recordingTimer = setInterval(updateRecordingTime, 100);

    setState("recording");
    setStatus("00:00", "recording");
    hideResultsButton();
  } catch (error) {
    setStatus("Microphone access denied", "error");
    setTimeout(hideStatus, 3000);
  }
}

// Stop recording and create WAV blob (does NOT close the shared context)
function stopRecording() {
  if (recorderNode) {
    recorderNode.disconnect();
    recorderNode = null;
  }
  // Use the shared context's sample rate for the WAV
  const rate = sharedCtx?.sampleRate || 48000;
  if (hasAudio(recordedSamples)) {
    audioBlob = createWavBlob(recordedSamples, rate);
  } else {
    audioBlob = null;
  }
  recordedSamples = [];
  if (mediaStream) {
    mediaStream.getTracks().forEach((track) => track.stop());
    mediaStream = null;
  }
  if (recordingTimer) {
    clearInterval(recordingTimer);
    recordingTimer = null;
  }
}

// Generate a tiny silent WAV to prime iOS audio session after recording
function generateSilenceWav(): Blob {
  const rate = 22050;
  const samples = 2205; // 0.1s of silence
  const buf = new ArrayBuffer(44 + samples * 2);
  const v = new DataView(buf);
  v.setUint32(0, 0x52494646, false); v.setUint32(4, 36 + samples * 2, true);
  v.setUint32(8, 0x57415645, false); v.setUint32(12, 0x666d7420, false);
  v.setUint32(16, 16, true); v.setUint16(20, 1, true); v.setUint16(22, 1, true);
  v.setUint32(24, rate, true); v.setUint32(28, rate * 2, true);
  v.setUint16(32, 2, true); v.setUint16(34, 16, true);
  v.setUint32(36, 0x64617461, false); v.setUint32(40, samples * 2, true);
  // All zeros = silence
  return new Blob([buf], { type: "audio/wav" });
}

const silenceBlob = generateSilenceWav();

// A single reusable Audio element, unlocked during the user gesture.
// iOS Safari only allows Audio elements started in a gesture to play.
// By reusing this element (swapping src), we avoid creating new elements later.
let gestureAudio: HTMLAudioElement | null = null;
let gestureAudioUrl: string | null = null;

function startGestureAudio() {
  stopGestureAudio();
  gestureAudioUrl = URL.createObjectURL(silenceBlob);
  gestureAudio = new Audio(gestureAudioUrl);
  gestureAudio.loop = true;
  gestureAudio.play().catch(() => {});
}

function playOnGestureAudio(blob: Blob) {
  if (!gestureAudio) return;
  // Clean up old URL
  if (gestureAudioUrl) {
    URL.revokeObjectURL(gestureAudioUrl);
  }
  gestureAudio.loop = false;
  gestureAudioUrl = URL.createObjectURL(blob);
  gestureAudio.src = gestureAudioUrl;
  gestureAudio.play().catch(() => {});
}

function stopGestureAudio() {
  if (gestureAudio) {
    gestureAudio.pause();
    gestureAudio.removeAttribute("src");
    gestureAudio.load();
    gestureAudio = null;
  }
  if (gestureAudioUrl) {
    URL.revokeObjectURL(gestureAudioUrl);
    gestureAudioUrl = null;
  }
}

// Stop recording and send
async function stopAndSend() {
  stopRecording();
  startGestureAudio(); // unlock Audio element during gesture for later playback
  await sendAudio();
}

// Cancel recording
function cancelRecording() {
  stopRecording();
  audioBlob = null;
  setState("idle");
  hideStatus();
}

// Play base64-encoded WAV audio.
// If a gesture-unlocked Audio element exists, reuse it (required on iOS Safari
// where new Audio elements can't play outside user gestures).
// Otherwise fall back to creating a new Audio element (works on desktop/non-iOS).
function playBase64Audio(b64: string) {
  const binaryString = atob(b64);
  const bytes = new Uint8Array(binaryString.length);
  for (let i = 0; i < binaryString.length; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  const blob = new Blob([bytes], { type: "audio/wav" });

  if (gestureAudio) {
    playOnGestureAudio(blob);
  } else {
    const url = URL.createObjectURL(blob);
    const audio = new Audio(url);
    audio.onended = () => URL.revokeObjectURL(url);
    audio.onerror = () => URL.revokeObjectURL(url);
    audio.play().catch(() => {});
  }
}

// --- Chime for tool execution ---
let chimeInterval: ReturnType<typeof setInterval> | null = null;

// Generate a tiny WAV with a two-tone chime (played via Audio element)
function generateChimeWav(): Blob {
  const rate = 22050;
  const duration = 0.4;
  const samples = Math.floor(rate * duration);
  const buffer = new ArrayBuffer(44 + samples * 2);
  const view = new DataView(buffer);
  // WAV header
  view.setUint32(0, 0x52494646, false);
  view.setUint32(4, 36 + samples * 2, true);
  view.setUint32(8, 0x57415645, false);
  view.setUint32(12, 0x666d7420, false);
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, rate, true);
  view.setUint32(28, rate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  view.setUint32(36, 0x64617461, false);
  view.setUint32(40, samples * 2, true);
  for (let i = 0; i < samples; i++) {
    const t = i / rate;
    const envelope = Math.exp(-t * 8); // fast decay
    const val = (Math.sin(2 * Math.PI * 523 * t) + Math.sin(2 * Math.PI * 659 * t)) * 0.08 * envelope;
    view.setInt16(44 + i * 2, Math.max(-1, Math.min(1, val)) * 32767, true);
  }
  return new Blob([buffer], { type: "audio/wav" });
}

// Pre-generate chime blob so we don't rebuild it every time
const chimeBlob = generateChimeWav();

// Play chime and interim audio through the gesture-unlocked Audio element.
// On iOS Safari, only the gesture-unlocked element can play; creating new
// Audio elements outside gestures fails silently.
function playChime() {
  if (!gestureAudio) return;
  playOnGestureAudio(chimeBlob);
}

function startChime() {
  playChime();
  chimeInterval = setInterval(playChime, 2500);
}

function stopChime() {
  if (chimeInterval) {
    clearInterval(chimeInterval);
    chimeInterval = null;
  }
}

// --- Agent status via WebSocket ---
let agentChiming = false;

function handleAgentStatus(msg: Record<string, unknown>) {
  console.log("[agent_status]", msg.state, msg);
  if (msg.state === "transcribed") {
    if (msg.text) {
      showTranscription(msg.text as string);
      showThinking();
    }
  } else if (msg.state === "searching") {
    setStatus("Searching...", "sending");
    // Play interim spoken response if included
    if (msg.ttsAudio) {
      playBase64Audio(msg.ttsAudio as string);
    }
    // Start chime after a delay so interim audio plays first
    if (!agentChiming) {
      agentChiming = true;
      setTimeout(() => {
        if (agentChiming) startChime();
      }, 1800);
    }
  } else if (msg.state === "thinking") {
    setStatus("Thinking...", "sending");
  }
}

// Send audio to server
async function sendAudio() {
  const target = machineSelect.value;

  // In relay mode, require a target
  if (currentMode === "relay" && !target) {
    setStatus("Select a device first", "error");
    setState("idle");
    setTimeout(hideStatus, 2000);
    return;
  }

  if (!audioBlob || audioBlob.size < 1000) {
    // No audio or too short (< ~30ms) — silently return to idle
    stopGestureAudio();
    audioBlob = null;
    setState("idle");
    hideStatus();
    return;
  }

  try {
    setState("sending");
    setStatus(currentMode === "talk" ? "Thinking..." : "Sending...", "sending");

    const formData = new FormData();
    formData.append("audio", audioBlob, "recording.wav");
    formData.append("mode", currentMode);
    formData.append("sessionId", SESSION_ID);
    if (currentMode === "relay") {
      formData.append("target", target);
    }

    const fetchStart = Date.now();
    const response = await fetch(`${API_BASE}/transcribe`, {
      method: "POST",
      body: formData,
    });

    const result = await response.json();
    const totalMs = Date.now() - fetchStart;

    // Stop chime if it was playing (talk mode with tool calls)
    if (agentChiming) {
      agentChiming = false;
      stopChime();
    }

    // Hide transcription text when response arrives
    hideTranscription();

    if (response.ok && result.noSpeech) {
      stopGestureAudio();
      audioBlob = null;
      setState("idle");
      hideStatus();
    } else if (response.ok && result.mode === "talk") {
      setStatus("Done!", "success");
      setTimeout(hideStatus, 2000);
      showTalkResults(result.rawText, result.agentResponse, result.sttMs, result.agentMs, totalMs, result.timings);
      if (result.ttsAudio) {
        // playBase64Audio reuses the gesture-unlocked Audio element
        playBase64Audio(result.ttsAudio);
      } else {
        stopGestureAudio();
      }
      audioBlob = null;
      setState("idle");
    } else if (response.ok) {
      setStatus("Sent!", "success");
      setTimeout(hideStatus, 2000);
      showRelayResults(result.rawText, result.cleanedText, result.sttMs, result.llmMs, totalMs);
      if (result.ttsAudio) {
        playBase64Audio(result.ttsAudio);
      } else {
        stopGestureAudio();
      }
      audioBlob = null;
      setState("idle");
    } else {
      stopGestureAudio();
      setStatus(result.error || "Failed to send", "error");
      setState("idle");
      setTimeout(hideStatus, 3000);
    }
  } catch (error) {
    stopGestureAudio();
    agentChiming = false;
    stopChime();
    setStatus("Network error", "error");
    setState("idle");
    setTimeout(hideStatus, 3000);
  }
}

// Press-and-hold interaction:
// - Press down: start recording immediately
// - Hold > 1s then release: walkie-talkie mode, sends on release
// - Tap < 1s then release: toggle mode, tap again to stop and send
let pressStartTime = 0;
let isHolding = false;

function handlePressDown(e: Event) {
  e.preventDefault();
  if (currentState === "idle") {
    pressStartTime = Date.now();
    isHolding = true;
    primeAudio(); // unlock audio playback on mobile during user gesture
    startRecording();
  } else if (currentState === "recording" && !isHolding) {
    // Toggle mode: second tap stops and sends
    stopAndSend();
  }
}

function handlePressUp(e: Event) {
  e.preventDefault();
  if (!isHolding || currentState !== "recording") return;
  isHolding = false;

  const holdDuration = Date.now() - pressStartTime;
  if (holdDuration > 1000) {
    // Walkie-talkie: held long enough, send immediately
    stopAndSend();
  }
  // Otherwise: short tap, stay in recording (toggle mode)
}

// Mouse events
mainBtn.addEventListener("mousedown", handlePressDown);
mainBtn.addEventListener("mouseup", handlePressUp);

// Touch events
mainBtn.addEventListener("touchstart", handlePressDown, { passive: false });
mainBtn.addEventListener("touchend", handlePressUp, { passive: false });

cancelBtn.addEventListener("click", (e) => {
  e.preventDefault();
  cancelRecording();
});

// Open results overlay
viewResultsBtn.addEventListener("click", openOverlay);

// Close overlay
closeOverlayBtn.addEventListener("click", closeOverlay);
resultsOverlay.querySelector(".overlay-backdrop")?.addEventListener("click", closeOverlay);

// Helper to send text
async function sendText(text: string) {
  const target = machineSelect.value;
  if (!text || !target) return;

  closeOverlay();

  try {
    setStatus("Sending...", "sending");

    const response = await fetch(`${API_BASE}/send-text`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ target, text }),
    });

    if (response.ok) {
      setStatus("Sent!", "success");
      setTimeout(hideStatus, 2000);
    } else {
      const result = await response.json();
      setStatus(result.error || "Failed to send", "error");
      setTimeout(hideStatus, 3000);
    }
  } catch (error) {
    setStatus("Network error", "error");
    setTimeout(hideStatus, 3000);
  }
}

// Resend buttons
resendCleanedBtn.addEventListener("click", () => sendText(lastCleanedText));
resendRawBtn.addEventListener("click", () => sendText(lastRawText));

// --- Settings / Voice selector ---
const VOICE_PRESETS = [
  { id: "en_US-lessac-high", name: "Lessac", desc: "Neutral American (default)" },
  { id: "en_US-amy-medium", name: "Amy", desc: "Warm American female" },
  { id: "en_US-ryan-high", name: "Ryan", desc: "Clear American male" },
  { id: "en_US-joe-medium", name: "Joe", desc: "Casual American male" },
  { id: "en_US-kristin-medium", name: "Kristin", desc: "American female" },
  { id: "en_GB-alba-medium", name: "Alba", desc: "Scottish English" },
  { id: "en_GB-cori-high", name: "Cori", desc: "British English female" },
  { id: "en_GB-jenny_dioco-medium", name: "Jenny", desc: "British English female" },
  { id: "en_GB-northern_english_male-medium", name: "Northern", desc: "Northern English male" },
];

let currentVoice = "en_US-lessac-high";

function openSettings() {
  settingsOverlay.classList.add("visible");
  document.body.style.overflow = "hidden";
  loadCurrentVoice();
}

function closeSettings() {
  settingsOverlay.classList.remove("visible");
  document.body.style.overflow = "";
}

async function loadCurrentVoice() {
  try {
    const resp = await fetch(`${API_BASE}/tts-voice`);
    if (resp.ok) {
      const data = await resp.json();
      if (data.voice) currentVoice = data.voice;
    }
  } catch {}
  renderVoiceList();
}

function renderVoiceList() {
  voiceListEl.innerHTML = VOICE_PRESETS.map((v) => `
    <div class="voice-item${v.id === currentVoice ? " active" : ""}" data-voice="${v.id}">
      <div class="voice-item-select" data-voice="${v.id}">
        <div class="voice-item-check">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="20 6 9 17 4 12"/>
          </svg>
        </div>
        <div class="voice-item-info">
          <div class="voice-item-name">${v.name}</div>
          <div class="voice-item-desc">${v.desc}</div>
        </div>
      </div>
      <div class="voice-preview-btn" role="button" tabindex="0" data-voice="${v.id}" aria-label="Preview ${v.name}">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" stroke="none">
          <polygon points="5 3 19 12 5 21 5 3"/>
        </svg>
      </div>
    </div>
  `).join("");

  // Select voice on select area tap
  voiceListEl.querySelectorAll(".voice-item-select").forEach((el) => {
    el.addEventListener("click", () => {
      const voice = (el as HTMLElement).dataset.voice!;
      selectVoice(voice);
    });
  });

  // Preview button — does NOT select
  voiceListEl.querySelectorAll(".voice-preview-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      const voice = (btn as HTMLElement).dataset.voice!;
      previewVoice(voice, btn as HTMLElement);
    });
  });
}

async function selectVoice(voice: string) {
  if (voice === currentVoice) return;

  // Optimistic update
  currentVoice = voice;
  renderVoiceList();

  try {
    const resp = await fetch(`${API_BASE}/tts-voice`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ voice }),
    });
    if (!resp.ok) {
      const data = await resp.json();
      console.error("Voice change failed:", data.error);
    }
  } catch (err) {
    console.error("Voice change failed:", err);
  }
}

async function previewVoice(voice: string, btn: HTMLElement) {
  // Show loading state
  btn.classList.add("loading");
  btn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>`;

  try {
    const resp = await fetch(`${API_BASE}/tts-preview`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: "Hello, this is how I sound.", voice }),
    });
    if (resp.ok) {
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const audio = new Audio(url);
      audio.play().catch(() => {});
      audio.onended = () => URL.revokeObjectURL(url);
    }
  } catch {}

  // Restore button
  btn.classList.remove("loading");
  btn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" stroke="none"><polygon points="5 3 19 12 5 21 5 3"/></svg>`;
}

settingsBtn.addEventListener("click", openSettings);
closeSettingsBtn.addEventListener("click", closeSettings);
settingsOverlay.querySelector(".overlay-backdrop")?.addEventListener("click", closeSettings);

// Dev-only test button: generates a WAV with tones and sends through normal flow
if (__APP_VERSION__ === "local dev") {
  const testBtn = document.createElement("button");
  testBtn.textContent = "Test Talk";
  testBtn.style.cssText = "position:fixed;bottom:8px;right:8px;z-index:999;padding:8px 14px;background:#ffa500;color:#000;border:none;border-radius:8px;font-family:inherit;font-size:0.8rem;font-weight:600;cursor:pointer;opacity:0.8";
  testBtn.addEventListener("click", async () => {
    testBtn.disabled = true;
    testBtn.textContent = "Generating...";
    try {
      // Use Piper TTS to generate a real speech WAV
      const resp = await fetch(`${API_BASE}/tts-preview`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text: "What is the weather like in Seattle today?" }),
      });
      if (!resp.ok) throw new Error("TTS failed");
      audioBlob = await resp.blob();
      if (currentMode !== "talk") setMode("talk");
      setState("sending");
      setStatus("Thinking...", "sending");
      primeAudio();
      startGestureAudio();
      sendAudio();
    } catch (e) {
      setStatus("Test failed: " + e, "error");
      setTimeout(hideStatus, 3000);
    }
    testBtn.disabled = false;
    testBtn.textContent = "Test Talk";
  });
  document.body.appendChild(testBtn);
}

// Register service worker
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js").catch(() => {});
}

// Version chip
document.getElementById("version-chip")!.textContent = __APP_VERSION__;

// Initial load — observer WebSocket pushes machine list, HTTP fetch as fallback
connectObserver();
setMode(currentMode); // apply persisted mode
setState("idle");
hideStatus();

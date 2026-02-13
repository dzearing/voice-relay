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

  // Track cc-wrapper connectivity for card reply buttons
  const wasConnected = ccWrapperConnected;
  ccWrapperConnected = Array.from(machineSelect.options).some(o => o.value.includes("-claude"));
  if (wasConnected !== ccWrapperConnected && isNotifOverlayOpen()) {
    renderNotifCards();
  }
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
      } else if (msg.type === "notifications_updated") {
        fetchNotifications();
      } else if (msg.type === "question") {
        handleQuestionEvent(msg.question);
      } else if (msg.type === "question_answered") {
        handleQuestionAnswered(msg.question_id);
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

// Auto-play new notifications toggle
const autoplayNotifToggle = document.getElementById("autoplay-notif-toggle") as HTMLButtonElement;
// Default to true if never set
let autoplayNewNotifs = localStorage.getItem("voicerelay_autoplay_notifs") !== "false";
autoplayNotifToggle.setAttribute("aria-checked", String(autoplayNewNotifs));

autoplayNotifToggle.addEventListener("click", () => {
  autoplayNewNotifs = !autoplayNewNotifs;
  localStorage.setItem("voicerelay_autoplay_notifs", String(autoplayNewNotifs));
  autoplayNotifToggle.setAttribute("aria-checked", String(autoplayNewNotifs));
});

// Dev-only test button: generates a WAV with tones and sends through normal flow
if (__APP_VERSION__ === "local dev") {
  const testBtn = document.createElement("button");
  testBtn.textContent = "Test Talk";
  testBtn.style.cssText = "position:fixed;bottom:8px;right:8px;z-index:50;padding:8px 14px;background:#ffa500;color:#000;border:none;border-radius:8px;font-family:inherit;font-size:0.8rem;font-weight:600;cursor:pointer;opacity:0.8";
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

  const testNotifBtn = document.createElement("button");
  testNotifBtn.textContent = "Test Notif";
  testNotifBtn.style.cssText = "position:fixed;bottom:8px;right:120px;z-index:50;padding:8px 14px;background:#06b6d4;color:#000;border:none;border-radius:8px;font-family:inherit;font-size:0.8rem;font-weight:600;cursor:pointer;opacity:0.8";
  testNotifBtn.addEventListener("click", async () => {
    testNotifBtn.disabled = true;
    testNotifBtn.textContent = "Generating...";
    try {
      const resp = await fetch(`${API_BASE}/notifications/test`, { method: "POST" });
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({ error: "Failed" }));
        throw new Error(err.error || "Failed");
      }
      const data = await resp.json();
      testNotifBtn.textContent = data.title || "Sent!";
      setTimeout(() => { testNotifBtn.textContent = "Test Notif"; }, 2000);
    } catch (e) {
      testNotifBtn.textContent = "Error";
      console.error("Test notif failed:", e);
      setTimeout(() => { testNotifBtn.textContent = "Test Notif"; }, 2000);
    }
    testNotifBtn.disabled = false;
  });
  document.body.appendChild(testNotifBtn);
}

// ── Notifications (Overlay + Carousel) ──

const notifOverlay = document.getElementById("notif-overlay") as HTMLDivElement;
const notifDetailsPanel = document.getElementById("notif-details-panel") as HTMLDivElement;
const notifCarousel = document.getElementById("notif-carousel") as HTMLDivElement;
const notifDots = document.getElementById("notif-dots") as HTMLDivElement;
const notifDismissAllBtn = document.getElementById("notif-dismiss-all-btn") as HTMLButtonElement;
const notifAutoplayBtn = document.getElementById("notif-autoplay-btn") as HTMLButtonElement;
const notifCountChip = document.getElementById("notif-count-chip") as HTMLSpanElement;
const notifBtn = document.getElementById("notif-btn") as HTMLButtonElement;
const notifCountEl = document.getElementById("notif-count") as HTMLSpanElement;
const notifReplyPanel = document.getElementById("notif-reply-panel") as HTMLDivElement;
const notifPlayBtn = document.getElementById("notif-play-btn") as HTMLButtonElement;

interface NotificationItem {
  id: string;
  title: string;
  summary: string;
  details?: string;
  priority?: string;
  source?: string;
  repo?: string;
  branch?: string;
  session?: string;
  reply_target?: string;
  created_at?: string;
  processed_at?: string;
  title_audio?: string;
  summary_audio?: string;
  details_audio?: string;
}

let notifItems: NotificationItem[] = [];
let knownNotifIds: Set<string> = new Set();
let notifIndex = 0;
let ccWrapperConnected = false;

// iOS Safari: single Audio element unlocked during user gesture
let notifAudioEl: HTMLAudioElement | null = null;
let notifAudioUrl: string | null = null;
let notifPlaying = false;
let notifAutoplayTimer: ReturnType<typeof setTimeout> | null = null;

// Played notification tracking
const PLAYED_KEY = "voicerelay_played_notifs";

function getPlayedIds(): Set<string> {
  try {
    return new Set<string>(JSON.parse(localStorage.getItem(PLAYED_KEY) || "[]"));
  } catch { return new Set(); }
}

function markPlayed(id: string) {
  const played = getPlayedIds();
  played.add(id);
  localStorage.setItem(PLAYED_KEY, JSON.stringify(Array.from(played)));
  const card = notifCarousel.querySelector(`.notif-card[data-id="${id}"]`);
  if (card && !card.classList.contains("played")) {
    card.classList.add("played");
  }
}

function clearPlayed() {
  localStorage.removeItem(PLAYED_KEY);
}

function isPlayed(id: string): boolean {
  return getPlayedIds().has(id);
}

function prunePlayedIds() {
  const played = getPlayedIds();
  const currentIds = new Set(notifItems.map(n => n.id));
  let changed = false;
  Array.from(played).forEach(id => {
    if (!currentIds.has(id)) {
      played.delete(id);
      changed = true;
    }
  });
  if (changed) {
    localStorage.setItem(PLAYED_KEY, JSON.stringify(Array.from(played)));
  }
}

function ensureNotifAudioEl() {
  if (!notifAudioEl) {
    notifAudioEl = new Audio();
    const silentUrl = URL.createObjectURL(silenceBlob);
    notifAudioEl.src = silentUrl;
    notifAudioEl.play().then(() => {
      notifAudioEl!.pause();
      URL.revokeObjectURL(silentUrl);
    }).catch(() => {
      URL.revokeObjectURL(silentUrl);
    });
  }
  return notifAudioEl;
}

function base64ToBlob(b64: string): Blob {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return new Blob([bytes], { type: "audio/wav" });
}

function escapeHtml(s: string): string {
  const div = document.createElement("div");
  div.textContent = s;
  return div.innerHTML;
}

function isNotifOverlayOpen() {
  return notifOverlay.classList.contains("visible");
}

function formatNotifSource(n: NotificationItem): string {
  let label = "";
  if (n.repo) {
    label = n.repo;
    if (n.branch) label += ` \u00B7 ${n.branch}`;
  } else if (n.source) {
    label = n.source.toUpperCase();
  }
  if (n.session) {
    label += (label ? " \u00B7 " : "") + `#${n.session}`;
  }
  return label;
}

// ── Overlay open/close ──

function openNotifOverlay() {
  ensureNotifAudioEl();
  notifOverlay.classList.add("visible");
  document.body.style.overflow = "hidden";

  // Find first unplayed notification
  const firstUnplayed = notifItems.findIndex(n => !isPlayed(n.id));
  if (firstUnplayed >= 0) {
    notifIndex = firstUnplayed;
  } else {
    // All played — go to replay card
    notifIndex = notifItems.length;
  }

  renderNotifCards();
  scrollToCard(notifIndex, "auto");
}

const ICON_CLOSE_SM = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`;

function hideDetailsPanel() {
  notifDetailsPanel.classList.remove("visible");
  stopNotifAudio();
}

/** Attach swipe-down-to-dismiss on a slide panel element. */
function setupPanelSwipeDismiss(panel: HTMLElement, onDismiss: () => void) {
  let startY = 0;
  let dragging = false;

  panel.addEventListener("touchstart", (e: TouchEvent) => {
    if (panel.scrollTop <= 0) {
      startY = e.touches[0].clientY;
      dragging = true;
    }
  }, { passive: true });

  panel.addEventListener("touchmove", (e: TouchEvent) => {
    if (!dragging) return;
    const dy = e.touches[0].clientY - startY;
    if (dy > 0) {
      panel.style.transform = `translateY(${dy}px)`;
      panel.style.opacity = String(Math.max(0, 1 - dy / 200));
    }
  }, { passive: true });

  panel.addEventListener("touchend", (e: TouchEvent) => {
    if (!dragging) return;
    dragging = false;
    const dy = e.changedTouches[0].clientY - startY;
    if (dy > 60) {
      onDismiss();
    }
    panel.style.transform = "";
    panel.style.opacity = "";
  });
}

function showDetailsPanel(text: string) {
  notifDetailsPanel.innerHTML = `<button class="notif-slide-panel-close" aria-label="Close">${ICON_CLOSE_SM}</button>${escapeHtml(text)}`;
  notifDetailsPanel.classList.add("visible");

  notifDetailsPanel.querySelector(".notif-slide-panel-close")!.addEventListener("click", hideDetailsPanel);
  setupPanelSwipeDismiss(notifDetailsPanel, hideDetailsPanel);
}

function closeNotifOverlay() {
  hideDetailsPanel();
  closeReplyPanel();
  notifOverlay.classList.remove("visible");
  document.body.style.overflow = "";
  stopNotifAudio();
  stopAutoplay();
  if (notifAudioEl) {
    notifAudioEl.pause();
    notifAudioEl.removeAttribute("src");
    notifAudioEl.load();
    notifAudioEl = null;
  }
  if (notifAudioUrl) {
    URL.revokeObjectURL(notifAudioUrl);
    notifAudioUrl = null;
  }
}

// ── Card rendering ──

const ICON_DISMISS = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`;
const ICON_REPLY = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 17 4 12 9 7"/><path d="M20 18v-2a4 4 0 0 0-4-4H4"/></svg>`;

function renderNotifCards() {
  if (notifItems.length === 0) {
    notifCarousel.innerHTML = `<div class="notif-empty">There are no more notifications.</div>`;
    notifDots.innerHTML = "";
    updateNotifCount();
    return;
  }

  notifCarousel.innerHTML = notifItems.map((n, i) => {
    const hasDetails = !!(n.details && n.details_audio);
    const played = isPlayed(n.id);
    return `<div class="notif-card${i === notifIndex ? " active" : ""}${played ? " played" : ""}" data-index="${i}" data-id="${n.id}">
      <div class="notif-card-actions">
        ${n.source === "claude-code" && ccWrapperConnected ? `<button class="notif-reply-btn" data-index="${i}" aria-label="Reply">${ICON_REPLY} Reply</button>` : ""}
        <button class="notif-card-close" data-id="${n.id}" aria-label="Dismiss">${ICON_DISMISS}</button>
      </div>
      ${played ? `<div class="notif-card-played-badge">Played</div>` : ""}
      ${formatNotifSource(n) ? `<div class="notif-card-source">${escapeHtml(formatNotifSource(n))}</div>` : ""}
      <div class="notif-card-title">${escapeHtml(n.title)}</div>
      <div class="notif-card-summary">${escapeHtml(n.summary)}</div>
      ${hasDetails ? `<button class="notif-see-more-btn" data-index="${i}">See more</button>` : ""}
    </div>`;
  }).join("") + `<div class="notif-card notif-replay-card${notifIndex === notifItems.length ? " active" : ""}" data-action="replay">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 4v6h6"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>
      <div>Replay All</div>
    </div>`;

  // Dot indicators (include replay card)
  const totalCards = notifItems.length + 1;
  notifDots.innerHTML = totalCards <= 1 ? "" : Array.from({length: totalCards}, (_, i) =>
    `<div class="notif-dot${i === notifIndex ? " active" : ""}"></div>`
  ).join("");

  // X dismiss buttons on cards
  notifCarousel.querySelectorAll(".notif-card-close").forEach((btn) => {
    btn.addEventListener("click", () => {
      const id = (btn as HTMLElement).dataset.id!;
      dismissNotification(id);
    });
  });

  // "See more" buttons — show details panel, stop audio, play details
  notifCarousel.querySelectorAll(".notif-see-more-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      const idx = parseInt((btn as HTMLElement).dataset.index!, 10);
      const item = notifItems[idx];
      if (!item) return;
      // Show details text in panel
      closeReplyPanel(); // close reply drawer if open
      if (item.details) {
        showDetailsPanel(item.details);
      }
      // Play details audio if available
      if (item.details_audio) {
        stopNotifAudio();
        stopAutoplay();
        playDetailsAudio(idx);
      }
    });
  });

  // Reply buttons (claude-code cards when wrapper is connected)
  notifCarousel.querySelectorAll(".notif-reply-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      const idx = parseInt((btn as HTMLElement).dataset.index!, 10);
      const item = notifItems[idx];
      if (item) openReplyPanel(item.summary, item.reply_target);
    });
  });

  // Replay All card handler
  const replayCard = notifCarousel.querySelector(".notif-replay-card");
  if (replayCard) {
    replayCard.addEventListener("click", () => {
      clearPlayed();
      notifIndex = 0;
      renderNotifCards();
      scrollToCard(0, "auto");
      if (autoplayNewNotifs) playNotifAudio(0);
    });
  }

  // Swipe-up to dismiss (not on replay card)
  notifCarousel.querySelectorAll(".notif-card:not(.notif-replay-card)").forEach((card) => {
    setupSwipeToDismiss(card as HTMLElement);
  });

  setupCarouselScroll();
  updateNotifCount();
}

// ── Swipe-up to dismiss ──

function setupSwipeToDismiss(card: HTMLElement) {
  let startX = 0;
  let startY = 0;
  let currentY = 0;
  let dragging = false;
  let committed = false; // true once we know this is a vertical dismiss gesture
  let rejected = false;  // true once we know this is a horizontal swipe
  let clone: HTMLElement | null = null;

  const COMMIT_THRESHOLD = 20; // px upward before creating clone

  card.addEventListener("touchstart", (e: TouchEvent) => {
    startX = e.touches[0].clientX;
    startY = e.touches[0].clientY;
    currentY = startY;
    dragging = true;
    committed = false;
    rejected = false;
  }, { passive: true });

  card.addEventListener("touchmove", (e: TouchEvent) => {
    if (!dragging || rejected) return;
    currentY = e.touches[0].clientY;
    const dx = e.touches[0].clientX - startX;
    const dy = currentY - startY;

    // If moving downward or not yet past threshold, wait
    if (dy >= 0) return;

    // Decide direction: if horizontal movement exceeds vertical, it's a carousel swipe
    if (!committed) {
      if (Math.abs(dx) > Math.abs(dy)) {
        rejected = true;
        return;
      }
      if (-dy < COMMIT_THRESHOLD) return;

      // Commit to vertical dismiss — create clone
      committed = true;
      const rect = card.getBoundingClientRect();
      clone = card.cloneNode(true) as HTMLElement;
      clone.style.cssText = `
        position: fixed;
        left: ${rect.left}px;
        top: ${rect.top}px;
        width: ${rect.width}px;
        height: ${rect.height}px;
        z-index: 200;
        pointer-events: none;
        transition: none;
        margin: 0;
        scroll-snap-align: none;
        background: #242435;
        border: 1px solid rgba(255,255,255,0.08);
        border-radius: 16px;
        box-sizing: border-box;
        box-shadow: 0 8px 32px rgba(0,0,0,0.5), 0 2px 8px rgba(0,0,0,0.3);
      `;
      notifOverlay.appendChild(clone);
      card.style.visibility = "hidden";
    }

    if (clone) {
      clone.style.transform = `translateY(${dy}px)`;
      clone.style.opacity = String(Math.max(0, 1 - Math.max(0, -dy - 100) / 100));
    }
  }, { passive: true });

  card.addEventListener("touchend", () => {
    if (!dragging) return;
    dragging = false;

    if (!committed || !clone) {
      // No clone was created — nothing to clean up
      clone = null;
      return;
    }

    const dy = currentY - startY;

    if (dy < -60) {
      // Dismiss — animate out
      clone.style.transition = "transform 0.25s ease-out, opacity 0.25s ease-out";
      clone.style.transform = "translateY(-120%)";
      clone.style.opacity = "0";
      const c = clone;
      setTimeout(() => c.remove(), 260);
      const id = card.dataset.id!;
      setTimeout(() => dismissNotification(id), 250);
    } else {
      // Snap back — instantly restore original, remove clone
      card.style.visibility = "";
      clone.remove();
    }
    clone = null;
  });
}

function updateActiveCard() {
  notifCarousel.querySelectorAll(".notif-card").forEach((card, i) => {
    card.classList.toggle("active", i === notifIndex);
  });
}

function updateNotifDots() {
  const dots = notifDots.querySelectorAll(".notif-dot");
  dots.forEach((d, i) => d.classList.toggle("active", i === notifIndex));
}

function scrollToCard(index: number, behavior: ScrollBehavior = "smooth") {
  const cards = notifCarousel.querySelectorAll(".notif-card");
  if (cards[index]) {
    (cards[index] as HTMLElement).scrollIntoView({ behavior, block: "nearest", inline: "center" });
    notifIndex = index;
    updateNotifDots();
    updateActiveCard();
  }
}

// ── Carousel scroll detection ──

function setupCarouselScroll() {
  let scrollTimer: ReturnType<typeof setTimeout> | null = null;

  const updateIndex = () => {
    const cards = notifCarousel.querySelectorAll(".notif-card");
    if (cards.length === 0) return;
    const containerLeft = notifCarousel.scrollLeft;
    const containerWidth = notifCarousel.clientWidth;
    const center = containerLeft + containerWidth / 2;
    let closest = 0;
    let closestDist = Infinity;
    cards.forEach((card, i) => {
      const el = card as HTMLElement;
      const cardCenter = el.offsetLeft + el.offsetWidth / 2;
      const dist = Math.abs(cardCenter - center);
      if (dist < closestDist) {
        closestDist = dist;
        closest = i;
      }
    });
    if (closest !== notifIndex) {
      // Mark the card we're swiping away from as played
      const prevItem = notifItems[notifIndex];
      if (prevItem) markPlayed(prevItem.id);

      notifIndex = closest;
      updateNotifDots();
      updateActiveCard();
      hideDetailsPanel();
      // User swiped — stop current audio, don't auto-play next
      stopNotifAudio();
      stopAutoplay();
    }
  };

  notifCarousel.addEventListener("scroll", () => {
    if (scrollTimer) clearTimeout(scrollTimer);
    scrollTimer = setTimeout(updateIndex, 150);
  }, { passive: true });
}

// ── Audio playback ──

function playNotifAudio(index: number) {
  const item = notifItems[index];
  if (!item) return;

  // Build queue: title then summary
  const queue: string[] = [];
  if (item.title_audio) queue.push(item.title_audio);
  if (item.summary_audio) queue.push(item.summary_audio);
  if (queue.length === 0) {
    // No audio — wait 1s then advance
    scheduleAutoAdvance();
    return;
  }

  stopNotifAudio();
  notifPlaying = true;
  updateNotifBtnState();

  const audio = ensureNotifAudioEl();
  let queueIndex = 0;
  let markedPlayed = false;

  const checkHalfPlayed = () => {
    if (!markedPlayed && audio.duration > 0 && audio.currentTime / audio.duration >= 0.5) {
      markedPlayed = true;
      markPlayed(item.id);
    }
  };
  activeTimeupdateHandler = checkHalfPlayed;

  const playNext = () => {
    audio.removeEventListener("timeupdate", checkHalfPlayed);
    if (queueIndex >= queue.length) {
      activeTimeupdateHandler = null;
      // Done with this card — wait 1s then advance
      notifPlaying = false;
      updateNotifBtnState();
      scheduleAutoAdvance();
      return;
    }
    const b64 = queue[queueIndex++];
    if (notifAudioUrl) URL.revokeObjectURL(notifAudioUrl);
    const blob = base64ToBlob(b64);
    notifAudioUrl = URL.createObjectURL(blob);
    audio.src = notifAudioUrl;
    audio.addEventListener("timeupdate", checkHalfPlayed);
    audio.play().catch(() => {});
    audio.onended = playNext;
  };

  playNext();
}

function playDetailsAudio(index: number) {
  const item = notifItems[index];
  if (!item?.details_audio) return;

  stopNotifAudio();
  notifPlaying = true;
  updateNotifBtnState();

  const audio = ensureNotifAudioEl();
  if (notifAudioUrl) URL.revokeObjectURL(notifAudioUrl);
  const blob = base64ToBlob(item.details_audio);
  notifAudioUrl = URL.createObjectURL(blob);
  audio.src = notifAudioUrl;

  let markedPlayed = false;
  const checkHalfPlayed = () => {
    if (!markedPlayed && audio.duration > 0 && audio.currentTime / audio.duration >= 0.5) {
      markedPlayed = true;
      markPlayed(item.id);
    }
  };
  activeTimeupdateHandler = checkHalfPlayed;
  audio.addEventListener("timeupdate", checkHalfPlayed);

  audio.play().catch(() => {});
  audio.onended = () => {
    audio.removeEventListener("timeupdate", checkHalfPlayed);
    activeTimeupdateHandler = null;
    notifPlaying = false;
    updateNotifBtnState();
    scheduleAutoAdvance();
  };
}

let activeTimeupdateHandler: (() => void) | null = null;

function stopNotifAudio() {
  if (notifAudioEl) {
    notifAudioEl.pause();
    notifAudioEl.onended = null;
    if (activeTimeupdateHandler) {
      notifAudioEl.removeEventListener("timeupdate", activeTimeupdateHandler);
      activeTimeupdateHandler = null;
    }
  }
  notifPlaying = false;
  updateNotifBtnState();
}

function scheduleAutoAdvance() {
  stopAutoplay();
  if (!autoplayNewNotifs) return;
  if (!isNotifOverlayOpen()) return;
  notifAutoplayTimer = setTimeout(() => {
    notifAutoplayTimer = null;
    if (!isNotifOverlayOpen()) return;
    // Find next unplayed notification after current index
    let next = -1;
    for (let i = notifIndex + 1; i < notifItems.length; i++) {
      if (!isPlayed(notifItems[i].id)) {
        next = i;
        break;
      }
    }
    if (next >= 0) {
      scrollToCard(next);
      playNotifAudio(next);
    } else {
      // No more unplayed — re-render to show replay card, scroll to it
      renderNotifCards();
      scrollToCard(notifItems.length, "smooth");
    }
  }, 1000);
}

function stopAutoplay() {
  if (notifAutoplayTimer) {
    clearTimeout(notifAutoplayTimer);
    notifAutoplayTimer = null;
  }
}

// ── Play/pause button ──

function updatePlayBtn() {
  notifPlayBtn.classList.toggle("playing", notifPlaying);
}

// ── State updates ──

function updateNotifBtnState() {
  const unplayedCount = notifItems.filter(n => !isPlayed(n.id)).length;
  updatePlayBtn();
  notifBtn.classList.toggle("has-notifs", unplayedCount > 0 && !notifPlaying);
  notifBtn.classList.toggle("playing", notifPlaying);
  notifCountEl.textContent = unplayedCount > 0 ? String(unplayedCount) : "";
}

function updateNotifCount() {
  notifCountChip.textContent = notifItems.length > 0 ? String(notifItems.length) : "";
}

// ── Fetch & dismiss ──

async function fetchNotifications() {
  try {
    const resp = await fetch(`${API_BASE}/notifications`);
    if (resp.ok) {
      const items = await resp.json();
      notifItems = Array.isArray(items) ? items : [];
    }
  } catch {
    notifItems = [];
  }

  const newItems = notifItems.filter((n) => !knownNotifIds.has(n.id));
  knownNotifIds = new Set(notifItems.map((n) => n.id));
  prunePlayedIds();

  updateNotifBtnState();
  updateNotifCount();

  // If overlay is open, re-render preserving position
  if (isNotifOverlayOpen()) {
    const prevId = notifItems[notifIndex]?.id;
    renderNotifCards();
    if (prevId) {
      const idx = notifItems.findIndex((n) => n.id === prevId);
      if (idx >= 0) {
        notifIndex = idx;
        scrollToCard(idx, "auto");
      }
    }
  }

  // Auto-play: if new items arrived and autoplay is on, open overlay and play.
  // Don't interrupt audio that's already playing — scheduleAutoAdvance will
  // pick up new unplayed notifications when the current one finishes.
  if (autoplayNewNotifs && newItems.length > 0 && !notifPlaying) {
    if (!isNotifOverlayOpen()) {
      openNotifOverlay();
    } else {
      const firstNew = notifItems.findIndex(n => n.id === newItems[0].id);
      if (firstNew >= 0) playNotifAudio(firstNew);
    }
  }
}

async function dismissNotification(id: string) {
  hideDetailsPanel();
  stopNotifAudio();
  stopAutoplay();

  // Animate card out
  const card = notifCarousel.querySelector(`.notif-card[data-id="${id}"]`);
  if (card) card.classList.add("dismissing");

  try {
    await fetch(`${API_BASE}/notifications/dismiss`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id }),
    });
  } catch {}

  notifItems = notifItems.filter((n) => n.id !== id);
  knownNotifIds.delete(id);
  // Clean up played state
  const played = getPlayedIds();
  if (played.has(id)) {
    played.delete(id);
    localStorage.setItem(PLAYED_KEY, JSON.stringify(Array.from(played)));
  }

  if (notifItems.length === 0) {
    renderNotifCards(); // shows empty state
  } else {
    if (notifIndex >= notifItems.length) notifIndex = notifItems.length - 1;
    renderNotifCards();
    scrollToCard(notifIndex, "auto");
    if (autoplayNewNotifs) playNotifAudio(notifIndex);
  }

  updateNotifBtnState();
  updateNotifCount();
}

async function dismissAllNotifications() {
  stopNotifAudio();
  stopAutoplay();
  try {
    await fetch(`${API_BASE}/notifications/dismiss-all`, { method: "POST" });
  } catch {}
  notifItems = [];
  knownNotifIds.clear();
  clearPlayed();
  renderNotifCards(); // shows empty state
  updateNotifBtnState();
  updateNotifCount();
}

// ── Claude Code Reply ──

function openReplyPanel(summary: string, replyTarget?: string) {
  hideDetailsPanel(); // close details if open
  notifReplyPanel.innerHTML = `
    <div class="notif-reply-header">
      <div class="notif-reply-header-text">Reply to: <span>${escapeHtml(summary)}</span></div>
      <button class="notif-slide-panel-close" aria-label="Close">${ICON_CLOSE_SM}</button>
    </div>
    <textarea id="notif-reply-input" placeholder="Type your reply..." rows="3"></textarea>
    <div class="notif-reply-actions">
      <button id="notif-reply-cancel-btn" class="notif-reply-cancel-btn">Cancel</button>
      <button id="notif-reply-send-btn" class="notif-reply-send-btn">Send</button>
    </div>`;
  notifReplyPanel.classList.add("visible");

  const input = document.getElementById("notif-reply-input") as HTMLTextAreaElement;
  const sendBtn = document.getElementById("notif-reply-send-btn") as HTMLButtonElement;
  const cancelBtn = document.getElementById("notif-reply-cancel-btn") as HTMLButtonElement;

  input.focus();

  sendBtn.addEventListener("click", () => sendCCReply(input, sendBtn, replyTarget));
  cancelBtn.addEventListener("click", closeReplyPanel);
  notifReplyPanel.querySelector(".notif-slide-panel-close")!.addEventListener("click", closeReplyPanel);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      sendCCReply(input, sendBtn, replyTarget);
    }
  });

  setupPanelSwipeDismiss(notifReplyPanel, closeReplyPanel);
}

function closeReplyPanel() {
  notifReplyPanel.classList.remove("visible");
}

async function sendCCReply(input: HTMLTextAreaElement, sendBtn: HTMLButtonElement, replyTarget?: string) {
  const text = input.value.trim();
  if (!text) return;

  // Use the notification's reply_target if available, otherwise fall back to machine dropdown
  let ccTarget = replyTarget || "";
  if (!ccTarget) {
    const target = machineSelect.value;
    if (target && target.includes("-claude")) {
      ccTarget = target;
    } else {
      const opt = Array.from(machineSelect.options).find((o) => o.value.includes("-claude"));
      if (!opt) {
        setStatus("cc-wrapper not connected", "error");
        setTimeout(hideStatus, 3000);
        return;
      }
      ccTarget = opt.value;
    }
  }

  sendBtn.disabled = true;
  try {
    const sendResp = await fetch(`${API_BASE}/send-text`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ target: ccTarget, text }),
    });

    if (sendResp.ok) {
      closeReplyPanel();
      setStatus("Reply sent!", "success");
      setTimeout(hideStatus, 2000);
    } else {
      const result = await sendResp.json();
      setStatus(result.error || "Failed to send reply", "error");
      setTimeout(hideStatus, 3000);
    }
  } catch {
    setStatus("Network error", "error");
    setTimeout(hideStatus, 3000);
  } finally {
    sendBtn.disabled = false;
  }
}

// ── Event listeners ──

notifBtn.addEventListener("click", () => {
  if (isNotifOverlayOpen()) {
    closeNotifOverlay();
  } else {
    openNotifOverlay();
  }
});

notifOverlay.querySelector(".overlay-backdrop")?.addEventListener("click", closeNotifOverlay);
notifDismissAllBtn.addEventListener("click", dismissAllNotifications);

// Play/pause button — plays or stops audio for current card
notifPlayBtn.addEventListener("click", () => {
  if (notifPlaying) {
    stopNotifAudio();
    stopAutoplay();
  } else if (notifIndex < notifItems.length) {
    playNotifAudio(notifIndex);
  }
});

// Auto-iterate toggle — when on, advances to next card after audio finishes
function updateAutoplayBtn() {
  notifAutoplayBtn.classList.toggle("active", autoplayNewNotifs);
}
updateAutoplayBtn();

notifAutoplayBtn.addEventListener("click", () => {
  autoplayNewNotifs = !autoplayNewNotifs;
  localStorage.setItem("voicerelay_autoplay_notifs", String(autoplayNewNotifs));
  autoplayNotifToggle.setAttribute("aria-checked", String(autoplayNewNotifs));
  updateAutoplayBtn();
  if (!autoplayNewNotifs) {
    stopNotifAudio();
    stopAutoplay();
  }
});

// ── AskUserQuestion Interception ──

const questionOverlay = document.getElementById("question-overlay") as HTMLDivElement;
const questionBody = document.getElementById("question-body") as HTMLDivElement;
const questionCloseBtn = document.getElementById("question-close-btn") as HTMLButtonElement;

interface QuestionOption {
  label: string;
  description: string;
}

interface QuestionItemData {
  question: string;
  header: string;
  options: QuestionOption[];
  multiSelect: boolean;
}

interface PendingQuestionData {
  id: string;
  reply_target: string;
  questions: QuestionItemData[];
  created_at: string;
  answered: boolean;
}

let pendingQuestions: PendingQuestionData[] = [];

function openQuestionOverlay() {
  questionOverlay.classList.add("visible");
  document.body.style.overflow = "hidden";
  renderQuestions();
}

function closeQuestionOverlay() {
  questionOverlay.classList.remove("visible");
  document.body.style.overflow = "";
}

function renderQuestions() {
  if (pendingQuestions.length === 0) {
    questionBody.innerHTML = `<div class="notif-empty">No pending questions.</div>`;
    return;
  }

  questionBody.innerHTML = pendingQuestions.map((pq) => {
    return pq.questions.map((q, qIdx) => {
      const optionsHtml = q.options.map((opt, optIdx) => {
        return `<button class="question-option-btn" data-qid="${pq.id}" data-opt-index="${optIdx}">
          <span>${escapeHtml(opt.label)}</span>
          ${opt.description ? `<span class="question-option-desc">${escapeHtml(opt.description)}</span>` : ""}
        </button>`;
      }).join("");

      // "Other" free-text input (AskUserQuestion always has an implicit "Other" option)
      const otherHtml = `<div class="question-other-row">
        <input class="question-other-input" data-qid="${pq.id}" placeholder="Other..." type="text" />
        <button class="question-other-send" data-qid="${pq.id}" data-other-index="${q.options.length}">Send</button>
      </div>`;

      return `<div class="question-item" data-qid="${pq.id}">
        ${q.header ? `<div class="question-header-chip">${escapeHtml(q.header)}</div>` : ""}
        <div class="question-text">${escapeHtml(q.question)}</div>
        <div class="question-options">${optionsHtml}</div>
        ${otherHtml}
      </div>`;
    }).join("");
  }).join("");

  // Wire up option buttons
  questionBody.querySelectorAll(".question-option-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      const el = btn as HTMLElement;
      const qid = el.dataset.qid!;
      const optIndex = parseInt(el.dataset.optIndex!, 10);
      answerQuestion(qid, optIndex, "");
    });
  });

  // Wire up "Other" send buttons
  questionBody.querySelectorAll(".question-other-send").forEach((btn) => {
    btn.addEventListener("click", () => {
      const el = btn as HTMLElement;
      const qid = el.dataset.qid!;
      const otherIndex = parseInt(el.dataset.otherIndex!, 10);
      const input = questionBody.querySelector(`.question-other-input[data-qid="${qid}"]`) as HTMLInputElement;
      const text = input?.value.trim();
      if (text) {
        answerQuestion(qid, otherIndex, text);
      }
    });
  });

  // Wire up Enter key on Other inputs
  questionBody.querySelectorAll(".question-other-input").forEach((input) => {
    input.addEventListener("keydown", (e: Event) => {
      const ke = e as KeyboardEvent;
      if (ke.key === "Enter") {
        ke.preventDefault();
        const el = input as HTMLInputElement;
        const qid = el.dataset.qid!;
        const sendBtn = questionBody.querySelector(`.question-other-send[data-qid="${qid}"]`) as HTMLElement;
        const otherIndex = parseInt(sendBtn.dataset.otherIndex!, 10);
        const text = el.value.trim();
        if (text) {
          answerQuestion(qid, otherIndex, text);
        }
      }
    });
  });
}

async function answerQuestion(questionId: string, index: number, otherText: string) {
  // Disable all buttons for this question
  const item = questionBody.querySelector(`.question-item[data-qid="${questionId}"]`);
  if (item) {
    item.querySelectorAll("button").forEach((b) => (b as HTMLButtonElement).disabled = true);
    // Highlight the selected option
    const selected = item.querySelector(`.question-option-btn[data-opt-index="${index}"]`);
    if (selected) selected.classList.add("selected");
  }

  try {
    const resp = await fetch(`${API_BASE}/question/answer`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        question_id: questionId,
        index,
        other_text: otherText,
      }),
    });

    if (resp.ok) {
      // Remove from local list and re-render
      pendingQuestions = pendingQuestions.filter((q) => q.id !== questionId);
      if (pendingQuestions.length === 0) {
        closeQuestionOverlay();
      } else {
        renderQuestions();
      }
      setStatus("Answer sent!", "success");
      setTimeout(hideStatus, 2000);
    } else {
      const result = await resp.json();
      setStatus(result.error || "Failed to send answer", "error");
      setTimeout(hideStatus, 3000);
      // Re-enable buttons
      if (item) item.querySelectorAll("button").forEach((b) => (b as HTMLButtonElement).disabled = false);
    }
  } catch {
    setStatus("Network error", "error");
    setTimeout(hideStatus, 3000);
    if (item) item.querySelectorAll("button").forEach((b) => (b as HTMLButtonElement).disabled = false);
  }
}

function handleQuestionEvent(pq: PendingQuestionData) {
  // Add to pending list if not already there
  if (!pendingQuestions.find((q) => q.id === pq.id)) {
    pendingQuestions.push(pq);
  }
  // Auto-open the question overlay
  openQuestionOverlay();
}

function handleQuestionAnswered(questionId: string) {
  pendingQuestions = pendingQuestions.filter((q) => q.id !== questionId);
  if (isQuestionOverlayOpen() && pendingQuestions.length === 0) {
    closeQuestionOverlay();
  } else if (isQuestionOverlayOpen()) {
    renderQuestions();
  }
}

function isQuestionOverlayOpen() {
  return questionOverlay.classList.contains("visible");
}

async function fetchPendingQuestions() {
  try {
    const resp = await fetch(`${API_BASE}/questions`);
    if (resp.ok) {
      const items = await resp.json();
      pendingQuestions = Array.isArray(items) ? items : [];
      if (pendingQuestions.length > 0) {
        openQuestionOverlay();
      }
    }
  } catch {}
}

questionCloseBtn.addEventListener("click", closeQuestionOverlay);
questionOverlay.querySelector(".overlay-backdrop")?.addEventListener("click", closeQuestionOverlay);

// Register service worker
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js").catch(() => {});
}

// Version chip
document.getElementById("version-chip")!.textContent = __APP_VERSION__;

// Unlock audio on first user interaction for autoplay
const unlockOnce = () => {
  ensureNotifAudioEl();
  document.removeEventListener("pointerdown", unlockOnce, true);
};
document.addEventListener("pointerdown", unlockOnce, true);

// Initial load
connectObserver();
setMode(currentMode);
setState("idle");
hideStatus();
fetchNotifications();
fetchPendingQuestions();

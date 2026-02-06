// Elements
const machineSelect = document.getElementById("machine") as HTMLSelectElement;
const refreshBtn = document.getElementById("refresh-btn") as HTMLButtonElement;
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

// State
type AppState = "idle" | "recording" | "sending";
let currentState: AppState = "idle";
let audioContext: AudioContext | null = null;
let mediaStream: MediaStream | null = null;
let audioWorkletNode: ScriptProcessorNode | null = null;
let recordedSamples: Float32Array[] = [];
let audioBlob: Blob | null = null;
let recordingStartTime: number = 0;
let recordingTimer: ReturnType<typeof setInterval> | null = null;

const SAMPLE_RATE = 16000;

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

// Store current results for resending
let lastRawText = "";
let lastCleanedText = "";

function showResultsButton(rawText: string, cleanedText: string) {
  lastRawText = rawText;
  lastCleanedText = cleanedText;
  cleanedTextEl.textContent = cleanedText;
  rawTextEl.textContent = rawText;
  viewResultsBtn.classList.add("visible");
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

// Fetch available machines
async function loadMachines() {
  try {
    machineSelect.innerHTML = '<option value="">Loading...</option>';

    const response = await fetch(`${API_BASE}/machines`);
    const machines = await response.json();

    if (machines.length === 0) {
      machineSelect.innerHTML = '<option value="">No devices connected</option>';
      return;
    }

    machineSelect.innerHTML = machines
      .map((m: { name: string }) => `<option value="${m.name}">${m.name}</option>`)
      .join("");
  } catch (error) {
    machineSelect.innerHTML = '<option value="">Connection failed</option>';
    setStatus("Connection failed", "error");
  }
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

// Start recording
async function startRecording() {
  try {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { sampleRate: SAMPLE_RATE, channelCount: 1, echoCancellation: true },
    });

    mediaStream = stream;
    audioContext = new AudioContext({ sampleRate: SAMPLE_RATE });
    const source = audioContext.createMediaStreamSource(stream);

    // Use ScriptProcessorNode (widely supported) to capture raw PCM
    const bufferSize = 4096;
    audioWorkletNode = audioContext.createScriptProcessor(bufferSize, 1, 1);
    recordedSamples = [];

    audioWorkletNode.onaudioprocess = (e) => {
      const input = e.inputBuffer.getChannelData(0);
      recordedSamples.push(new Float32Array(input));
    };

    source.connect(audioWorkletNode);
    audioWorkletNode.connect(audioContext.destination);

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

// Stop recording and create WAV blob
function stopRecording() {
  if (audioWorkletNode) {
    audioWorkletNode.disconnect();
    audioWorkletNode = null;
  }
  if (audioContext) {
    const rate = audioContext.sampleRate;
    audioContext.close();
    audioContext = null;
    audioBlob = createWavBlob(recordedSamples, rate);
    recordedSamples = [];
  }
  if (mediaStream) {
    mediaStream.getTracks().forEach((track) => track.stop());
    mediaStream = null;
  }
  if (recordingTimer) {
    clearInterval(recordingTimer);
    recordingTimer = null;
  }
}

// Stop recording and send
async function stopAndSend() {
  stopRecording();
  await sendAudio();
}

// Cancel recording
function cancelRecording() {
  stopRecording();
  audioBlob = null;
  setState("idle");
  hideStatus();
}

// Send audio to server
async function sendAudio() {
  const target = machineSelect.value;

  if (!target) {
    setStatus("Select a device first", "error");
    setState("idle");
    setTimeout(hideStatus, 2000);
    return;
  }

  if (!audioBlob) {
    setStatus("No audio recorded", "error");
    setState("idle");
    setTimeout(hideStatus, 2000);
    return;
  }

  try {
    setState("sending");
    setStatus("Sending...", "sending");

    const formData = new FormData();
    formData.append("audio", audioBlob, "recording.wav");
    formData.append("target", target);

    const response = await fetch(`${API_BASE}/transcribe`, {
      method: "POST",
      body: formData,
    });

    const result = await response.json();

    if (response.ok) {
      setStatus("Sent!", "success");
      setTimeout(hideStatus, 2000);
      showResultsButton(result.rawText, result.cleanedText);
      audioBlob = null;
      setState("idle");
    } else {
      setStatus(result.error || "Failed to send", "error");
      setState("idle");
      setTimeout(hideStatus, 3000);
    }
  } catch (error) {
    setStatus("Network error", "error");
    setState("idle");
    setTimeout(hideStatus, 3000);
  }
}

// Main button click handler
function handleMainButtonClick() {
  switch (currentState) {
    case "idle":
      startRecording();
      break;
    case "recording":
      stopAndSend();
      break;
  }
}

// Event listeners
mainBtn.addEventListener("click", (e) => {
  e.preventDefault();
  handleMainButtonClick();
});

mainBtn.addEventListener("touchend", (e) => {
  e.preventDefault();
  handleMainButtonClick();
});

cancelBtn.addEventListener("click", (e) => {
  e.preventDefault();
  cancelRecording();
});

refreshBtn.addEventListener("click", loadMachines);

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

// Register service worker
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("/sw.js").catch(() => {});
}

// Initial load
loadMachines();
setState("idle");
hideStatus();

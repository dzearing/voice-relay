import express from "express";
import { createServer } from "http";
import { WebSocketServer, WebSocket } from "ws";
import multer from "multer";
import path from "path";
import { fileURLToPath } from "url";
import { registry } from "./registry.js";
import { transcribe } from "./stt.js";
import { cleanupText } from "./ollama.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PORT = parseInt(process.env.PORT || "53937");

const app = express();
const server = createServer(app);
const wss = new WebSocketServer({ server, path: "/ws" });

// Multer for file uploads
const upload = multer({ storage: multer.memoryStorage() });

// Serve PWA static files (built output)
const pwaPath = path.resolve(__dirname, "../../pwa/dist");
app.use(express.static(pwaPath));

// CORS for development
app.use((req, res, next) => {
  res.header("Access-Control-Allow-Origin", "*");
  res.header("Access-Control-Allow-Headers", "Content-Type");
  next();
});

// Health check
app.get("/health", (req, res) => {
  res.json({ status: "ok" });
});

// Send text directly to a target (for resending raw text)
app.post("/send-text", express.json(), (req, res) => {
  const { target, text } = req.body;

  if (!target || !text) {
    return res.status(400).json({ error: "Missing target or text" });
  }

  const sent = registry.sendText(target, text);
  if (!sent) {
    return res.status(404).json({ error: `Target machine '${target}' not connected` });
  }

  res.json({ success: true, text, target });
});

// List registered echo services
app.get("/machines", (req, res) => {
  res.json(registry.list());
});

// Transcribe endpoint
app.post("/transcribe", upload.single("audio"), async (req, res) => {
  try {
    const target = req.body?.target as string;
    const file = req.file;

    if (!file) {
      return res.status(400).json({ error: "No audio file provided" });
    }

    if (!target) {
      return res.status(400).json({ error: "No target machine specified" });
    }

    console.log(
      `Received audio: ${file.originalname}, size: ${file.size}, target: ${target}`
    );

    // 1. Transcribe audio
    const transcription = await transcribe(file.buffer, file.originalname || "audio.webm");
    console.log(`Raw transcription: ${transcription.text}`);

    // 2. Clean up text with Ollama
    const cleanedText = await cleanupText(transcription.text);
    console.log(`Cleaned text: ${cleanedText}`);

    // 3. Send to target echo service
    const sent = registry.sendText(target, cleanedText);
    if (!sent) {
      return res.status(404).json({
        error: `Target machine '${target}' not connected`,
        text: cleanedText,
      });
    }

    res.json({
      success: true,
      rawText: transcription.text,
      cleanedText,
      target,
    });
  } catch (error) {
    console.error("Transcription error:", error);
    res.status(500).json({ error: String(error) });
  }
});

// WebSocket handling for echo services
wss.on("connection", (ws: WebSocket) => {
  console.log("New WebSocket connection");

  ws.on("message", (data) => {
    try {
      const message = JSON.parse(data.toString());

      if (message.type === "register" && message.name) {
        registry.register(message.name, ws);
        ws.send(JSON.stringify({ type: "registered", name: message.name }));
      }
    } catch (error) {
      console.error("Invalid WebSocket message:", error);
    }
  });

  ws.on("close", () => {
    registry.unregister(ws);
  });

  ws.on("error", (error) => {
    console.error("WebSocket error:", error);
    registry.unregister(ws);
  });
});

server.listen(PORT, () => {
  console.log(`Coordinator running on port ${PORT}`);
  console.log(`PWA available at http://localhost:${PORT}`);
  console.log(`WebSocket endpoint: ws://localhost:${PORT}/ws`);
});

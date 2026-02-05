import WebSocket from "ws";
import { loadConfig } from "./config.js";
import { outputText } from "./keyboard.js";

const config = loadConfig();
let ws: WebSocket | null = null;
let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;

function connect(): void {
  console.log(`Connecting to coordinator at ${config.coordinatorUrl}...`);

  // Allow self-signed certificates for local development
  ws = new WebSocket(config.coordinatorUrl, {
    rejectUnauthorized: false,
  });

  ws.on("open", () => {
    console.log("Connected to coordinator");

    // Register this echo service
    ws!.send(JSON.stringify({ type: "register", name: config.name }));
  });

  ws.on("message", async (data) => {
    try {
      const message = JSON.parse(data.toString());

      if (message.type === "registered") {
        console.log(`Registered as: ${message.name}`);
      } else if (message.type === "text" && message.content) {
        console.log(`Received text: ${message.content}`);

        await outputText(message.content, {
          mode: config.outputMode,
          copy: config.copyToClipboard,
          delay: config.typeDelay,
        });
      }
    } catch (error) {
      console.error("Error processing message:", error);
    }
  });

  ws.on("close", () => {
    console.log("Disconnected from coordinator");
    scheduleReconnect();
  });

  ws.on("error", (error) => {
    console.error("WebSocket error:", error.message);
  });
}

function scheduleReconnect(): void {
  if (reconnectTimeout) {
    clearTimeout(reconnectTimeout);
  }

  console.log("Reconnecting in 5 seconds...");
  reconnectTimeout = setTimeout(() => {
    connect();
  }, 5000);
}

// Handle graceful shutdown
process.on("SIGINT", () => {
  console.log("\nShutting down...");
  if (ws) {
    ws.close();
  }
  process.exit(0);
});

// Start connection
console.log(`Echo Service: ${config.name}`);
console.log(`Output mode: ${config.outputMode}`);
if (config.outputMode === "type") {
  console.log(`Type delay: ${config.typeDelay}ms`);
}
console.log(`Copy to clipboard: ${config.copyToClipboard}`);
connect();

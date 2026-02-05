import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import path from "path";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export type OutputMode = "type" | "paste";

export interface Config {
  name: string;
  coordinatorUrl: string;
  outputMode: OutputMode;
  typeDelay: number;
  copyToClipboard: boolean;
}

export function loadConfig(): Config {
  const configPath = path.resolve(__dirname, "../config.json");

  try {
    const content = readFileSync(configPath, "utf-8");
    const config = JSON.parse(content);

    return {
      name: config.name || "Echo-Client",
      coordinatorUrl: config.coordinatorUrl || "ws://localhost:53937/ws",
      outputMode: config.outputMode === "paste" ? "paste" : "type",
      typeDelay: config.typeDelay ?? 20,
      copyToClipboard: config.copyToClipboard ?? true,
    };
  } catch (error) {
    console.error("Failed to load config, using defaults:", error);
    return {
      name: "Echo-Client",
      coordinatorUrl: "ws://localhost:53937/ws",
      outputMode: "type",
      typeDelay: 20,
      copyToClipboard: true,
    };
  }
}

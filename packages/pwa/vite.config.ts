import { defineConfig } from "vite";
import { copyFileSync } from "fs";
import { resolve } from "path";

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(process.env.APP_VERSION || "local dev"),
  },
  build: {
    outDir: "dist",
  },
  server: {
    port: 5001,
    proxy: {
      "/ws": {
        target: "ws://localhost:53937",
        ws: true,
      },
      "/transcribe": "http://localhost:53937",
      "/send-text": "http://localhost:53937",
      "/health": "http://localhost:53937",
      "/machines": "http://localhost:53937",
      "/connect": "http://localhost:53937",
      "/connect-info": "http://localhost:53937",
      "/tts-voice": "http://localhost:53937",
      "/tts-preview": "http://localhost:53937",
    },
  },
  plugins: [
    {
      name: "copy-pwa-assets",
      closeBundle() {
        // Copy PWA assets to dist
        copyFileSync(
          resolve(__dirname, "manifest.json"),
          resolve(__dirname, "dist/manifest.json")
        );
        copyFileSync(
          resolve(__dirname, "sw.js"),
          resolve(__dirname, "dist/sw.js")
        );
      },
    },
  ],
});

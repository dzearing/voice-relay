import { defineConfig } from "vite";
import { copyFileSync } from "fs";
import { resolve } from "path";

export default defineConfig({
  build: {
    outDir: "dist",
  },
  server: {
    port: 5001,
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

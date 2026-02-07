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

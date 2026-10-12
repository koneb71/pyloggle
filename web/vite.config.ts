/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

const api = process.env.PYLOGGLE_API ?? "http://127.0.0.1:8080";

export default defineConfig({
  base: "/ui/",
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
  server: {
    port: 5173,
    proxy: {
      "/ui/api": { target: api, changeOrigin: false },
      "/ui/login": { target: api, changeOrigin: false },
      "/ui/oauth2": { target: api, changeOrigin: false },
      "/ui/logout": { target: api, changeOrigin: false },
      "/v1": { target: api, changeOrigin: false },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: false,
    sourcemap: false,
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        // One vendor chunk: splitting react from its dependents creates circular chunk imports
        // ("Cannot set properties of undefined (setting 'Activity')") with React 19.
        manualChunks: (id) => (id.includes("node_modules") ? "vendor" : undefined),
      },
    },
  },
  test: { environment: "jsdom", globals: true, setupFiles: ["./vitest.setup.ts"], css: false },
});

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Khi dev: giao diện chạy ở cổng 5173, mọi request /api được chuyển sang
// backend Go ở cổng 8080 (make dev-api).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: { "/api": "http://127.0.0.1:8080" },
  },
  build: { outDir: "dist", emptyOutDir: true },
});

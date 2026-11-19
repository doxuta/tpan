import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Vite chạy file này bằng Node nên có process; khai báo tối thiểu thay vì
// thêm cả @types/node chỉ cho một dòng.
declare const process: { env: Record<string, string | undefined> };

// Khi dev: giao diện chạy ở cổng 5173, mọi request /api được chuyển sang
// backend Go ở cổng 18080 (make dev-api). Không dùng 8080 vì cổng đó hay bị chương trình khác trên máy dev chiếm. Biến TPAN_API đổi địa chỉ backend, để
// nhiều người chạy nhiều bản demo cùng lúc trên một máy.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: { "/api": process.env.TPAN_API ?? "http://127.0.0.1:18080" },
  },
  build: { outDir: "dist", emptyOutDir: true },
});

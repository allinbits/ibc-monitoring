import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The console is served by ibcmon itself; in development, /api is proxied to
// a running `ibcmon serve` (default :8080).
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": {
        target: process.env.IBCMON_API ?? "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
  build: { outDir: "dist", sourcemap: false },
  test: { setupFiles: ["src/test-setup.ts"], environment: "node", include: ["src/**/*.test.ts"] },
});

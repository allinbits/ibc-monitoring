import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The console is served by ibcmon itself; in development, /api is proxied to
// a running `ibcmon serve` (default :8080).
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: { "/api": process.env.IBCMON_API ?? "http://127.0.0.1:8080" },
  },
  build: { outDir: "dist", sourcemap: false },
  test: { environment: "node", include: ["src/**/*.test.ts"] },
});

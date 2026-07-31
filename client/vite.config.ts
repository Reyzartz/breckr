import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    // In dev the API runs separately; in production the Go server serves this
    // build from its own origin, so there is no CORS story in either case.
    proxy: {
      "/api": {
        target: "http://127.0.0.1:3000",
        changeOrigin: true,
        // /api/events is a websocket. Without this the upgrade is not
        // forwarded and the dashboard silently never updates.
        //
        // Note that changeOrigin rewrites Host but not Origin, so the server
        // does not see the handshake as same-origin and checks it against
        // CLIENT_ALLOWED_ORIGIN — which defaults to this dev server.
        ws: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});

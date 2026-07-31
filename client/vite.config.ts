import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";

export default defineConfig({
  plugins: [
    // Generates routeTree.gen.ts from src/routes/**; must run before
    // @vitejs/plugin-react so it sees the routes as written, not as transformed.
    tanstackRouter({ target: "react", autoCodeSplit: true }),
    react(),
    tailwindcss(),
  ],
  server: {
    port: 5173,
    // In dev the API runs separately; in production the Go server serves this
    // build from its own origin, so there is no CORS story in either case.
    proxy: {
      "/api": {
        target: "http://127.0.0.1:3000",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

// In dev, open http://<workspace>.handoff.localhost:5173; API calls proxy to the Go server.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    host: true,
    allowedHosts: [".localhost"],
    proxy: Object.fromEntries(
      ["/api", "/auth", "/oauth", "/mcp", "/.well-known"].map((p) => [p, { target: "http://localhost:8080", changeOrigin: false }]),
    ),
  },
});

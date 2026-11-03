import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

// Dev proxy mirrors nginx.conf: /api -> msapi, /ows -> mapserver (PLAN.md §11.1).
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const msapi = env.MSAPI_UPSTREAM || "http://localhost:8081";
  const mapserver = env.MAPSERVER_UPSTREAM || "http://localhost:8080";
  return {
    plugins: [react(), tailwindcss()],
    resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
    server: {
      port: 5173,
      proxy: {
        "/api": { target: msapi, changeOrigin: true },
        "/ows": { target: mapserver, changeOrigin: true, rewrite: (p) => p.replace(/^\/ows/, "/") },
      },
    },
    build: {
      chunkSizeWarningLimit: 900,
      // No source maps in the production image (do not publish the source tree).
      sourcemap: false,
      rollupOptions: {
        output: { manualChunks: { ol: ["ol"], codemirror: ["@uiw/react-codemirror", "@codemirror/lang-xml"] } },
      },
    },
  };
});

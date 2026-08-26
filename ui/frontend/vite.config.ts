import path from "path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Proxy API calls during `vite dev` to a running mission-control backend.
const apiTarget = process.env.INCIDENT_COMMANDER_API_URL || "http://localhost:8080";

export default defineConfig({
  base: "/ui/",
  plugins: [tailwindcss(), react()],
  // React and many libraries check `process.env.NODE_ENV` at runtime.
  define: {
    "process.env.NODE_ENV": JSON.stringify(process.env.NODE_ENV || "development"),
    "process.env": "{}",
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
    dedupe: ["react", "react-dom", "@tanstack/react-query"],
  },
  server: {
    proxy: {
      "/resources": apiTarget,
      "/schemas": apiTarget,
      "/catalog": apiTarget,
      "/config": apiTarget,
      "/db": apiTarget,
      "/playbook": apiTarget,
      "/auth": apiTarget,
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    minify: process.env.INCIDENT_COMMANDER_UI_RELEASE === "1",
    sourcemap: true,
    // Cap inline-asset size at 4KB; anything larger gets a separate file
    // (the Go server embeds the whole dist tree, so extra files are free).
    assetsInlineLimit: 4096,
  },
});

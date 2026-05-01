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
  // Vite only rewrites these when minifying; with minify=false the
  // references leak and the browser throws ReferenceError. Replace them
  // at bundle time so the shipped code is self-contained.
  define: {
    "process.env.NODE_ENV": JSON.stringify("production"),
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
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // Readable stack traces + clickable source locations in Chrome DevTools
    // when stepping through the bundled app. Flip these off for a production
    // tree-shake/minify pass (add INCIDENT_COMMANDER_UI_RELEASE=1).
    minify: process.env.INCIDENT_COMMANDER_UI_RELEASE === "1",
    // Inline the sourcemap as a data:// URI comment so it survives the
    // <script> inlining in ui/page.go — browsers extract it from the script
    // itself when there's no separate .map URL.
    sourcemap: process.env.INCIDENT_COMMANDER_UI_RELEASE === "1" ? true : "inline",
    // Single-file IIFE so the Go server can //go:embed one bundle and inline
    // it into the HTML shell. Matches ui/page.go's <script>{bundleJS}</script>.
    lib: {
      entry: path.resolve(__dirname, "src/main.tsx"),
      name: "IncidentCommanderUI",
      formats: ["iife"],
      fileName: () => "ui.js",
    },
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
      },
    },
  },
});

import { defineConfig } from "vite";
import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// `base: './'` keeps every emitted asset URL relative — required because
// the plugin SDK serves /api/plugins/sql-server/ui/ as the iframe origin
// and absolute-from-root URLs would 404.
export default defineConfig({
  plugins: [tailwindcss(), preact()],
  base: "./",
  // clicky-ui externalises React; preact/compat aliases it for runtime and
  // JSX. dedupe keeps a single hooks instance across clicky-ui and our app.
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
      react: "preact/compat",
      "react-dom": "preact/compat",
      "react/jsx-runtime": "preact/jsx-runtime",
    },
    dedupe: ["preact"],
  },
  define: {
    "process.env.NODE_ENV": JSON.stringify("production"),
    "process.env": "{}",
    // Build-time injected — printed on app startup and exposed via /version.
    __PLUGIN_VERSION__: JSON.stringify(process.env.PLUGIN_VERSION ?? "dev"),
    __PLUGIN_BUILD_DATE__: JSON.stringify(process.env.PLUGIN_BUILD_DATE ?? ""),
  },
  build: {
    // Vite builds straight into ../ui so the Go //go:embed in main.go picks
    // up the bundle without an extra copy step. emptyOutDir on a parent
    // directory requires explicit opt-in; we own the directory so it's safe.
    outDir: "../ui",
    emptyOutDir: true,
    sourcemap: true,
  },
});

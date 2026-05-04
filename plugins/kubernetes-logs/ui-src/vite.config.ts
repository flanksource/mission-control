import path from "path";
import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// Build to ../ui so the Go embed (//go:embed ui/*) picks up the bundle.
// The plugin's HTTP server serves these assets at the iframe root.
export default defineConfig({
  // Relative base so the bundled <script src> resolves against the plugin
  // proxy path (`/api/plugins/kubernetes-logs/ui/...`) instead of the host root.
  base: "./",
  plugins: [tailwindcss(), preact()],
  // clicky-ui ships ESM assuming React; preact/compat aliases it for runtime
  // and JSX. dedupe keeps a single hooks instance across clicky-ui and our app.
  resolve: {
    alias: {
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
    outDir: path.resolve(__dirname, "../ui"),
    emptyOutDir: true,
    minify: process.env.PLUGIN_UI_RELEASE === "1",
    sourcemap: true,
    rollupOptions: {
      input: path.resolve(__dirname, "index.html"),
      output: {
        entryFileNames: "assets/[name].js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
});

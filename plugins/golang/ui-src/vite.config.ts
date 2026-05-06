import { defineConfig } from "vite";
import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig({
  plugins: [tailwindcss(), preact()],
  base: "./",
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
    __PLUGIN_VERSION__: JSON.stringify(process.env.PLUGIN_VERSION ?? "dev"),
    __PLUGIN_BUILD_DATE__: JSON.stringify(process.env.PLUGIN_BUILD_DATE ?? ""),
  },
  build: {
    outDir: "../ui",
    emptyOutDir: true,
    sourcemap: true,
  },
});

import path from "path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Proxy API calls during `vite dev` to a running mission-control backend.
const apiTarget = process.env.INCIDENT_COMMANDER_API_URL || "http://localhost:8080";

// Vendor chunk groups — each maps a friendly chunk name to a list of
// substrings matched against the module path. Each group must be a complete
// dependency-closed unit; any shared transitive dep falls back to the
// catch-all `vendor` chunk, which Rollup warns about with circular-chunk
// errors when the split isn't clean.
const vendorChunks: Record<string, string[]> = {
  "vendor-react": ["/react/", "/react-dom/", "/scheduler/"],
  "vendor-query": ["/@tanstack/"],
  "vendor-clicky": ["/@flanksource/clicky-ui/", "/@flanksource/icons/"],
  "vendor-iconify": ["/@iconify/", "/iconify-icon/"],
  "vendor-radix": ["/@radix-ui/"],
  // Scalar's API reference + everything it transitively imports (Vue,
  // CodeMirror, Lezer grammars, floating-ui, headless-ui, ai-sdk, vueuse,
  // markdown/HAST pipeline, shiki + grammars). These all share utility
  // packages, so splitting them produces circular chunks. Keep the whole
  // graph in one bucket — the routes that need any of it pull in everything.
  "vendor-scalar": [
    "/shiki/",
    "/@shikijs/",
    "/@scalar/",
    "/@codemirror/",
    "/@lezer/",
    "/@marijn/",
    "/@floating-ui/",
    "/@headlessui/",
    "/@vue/",
    "/vue/",
    "/@vueuse/",
    "/@unhead/",
    "/@ai-sdk/",
    "/@replit/",
    "/@ungap/",
    "/remark-",
    "/rehype-",
    "/hast-util-",
    "/mdast-util-",
    "/micromark",
    "/unified/",
    "/unist-util-",
    "/property-information/",
    "/stringify-entities/",
    "/character-entities",
    "/space-separated-tokens/",
    "/comma-separated-tokens/",
    "/zwitch/",
    "/html-void-elements/",
    "/ccount/",
  ],
};

export default defineConfig({
  base: "/ui/",
  plugins: [tailwindcss(), react()],
  // React and many libraries check `process.env.NODE_ENV` at runtime.
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
    minify: process.env.INCIDENT_COMMANDER_UI_RELEASE === "1",
    sourcemap: true,
    // Cap inline-asset size at 4KB; anything larger gets a separate file
    // (the Go server embeds the whole dist tree, so extra files are free).
    assetsInlineLimit: 4096,
    rollupOptions: {
      output: {
        // Heaviest groups first so the explicit match wins. Anything in
        // node_modules that doesn't match falls into vendor-scalar — almost
        // every leftover dep is a scalar transitive (ai-sdk, vfile, radix-vue,
        // etc) and bundling them together avoids circular-chunk warnings.
        manualChunks(id) {
          if (!id.includes("node_modules")) return undefined;
          for (const [chunkName, patterns] of Object.entries(vendorChunks)) {
            if (patterns.some((p) => id.includes(p))) return chunkName;
          }
          return "vendor-scalar";
        },
      },
    },
  },
});

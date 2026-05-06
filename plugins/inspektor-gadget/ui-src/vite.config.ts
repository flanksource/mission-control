import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [tailwindcss(), react()],
  base: "./",
  build: {
    outDir: "../ui",
    emptyOutDir: true
  }
});

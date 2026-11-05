import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// Go embeds dist/ (main.go). The avatar, blob and frame routes come from Go,
// so there is no dev server here: build, then run the app.
export default defineConfig({
  plugins: [vue()],
  // "/src" is root-relative: Vite resolves it inside this folder
  resolve: { alias: { "@": "/src" } },
  build: {
    // WebKitGTK 2.44+ and Android System WebView: no legacy syntax needed
    target: "es2022",
    // dist/.gitkeep keeps `go build` working before the first frontend build
    emptyOutDir: false,
    chunkSizeWarningLimit: 900,
  },
});

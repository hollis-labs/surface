import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import { defineConfig } from "vite"

// The component gallery on its own page (gallery.html), with no app and no server: what
// `make design-preview` serves with hot reload and `make design-gallery` builds to static files.
// GALLERY_HOST and GALLERY_PORT say where it listens; GALLERY_OUT where a build goes.
const root = import.meta.dirname

export default defineConfig({
  root,
  base: "./",
  plugins: [tailwindcss()],
  resolve: {
    alias: {
      "@": resolve(root, "src"),
      "@hollis-labs/surface-page/sdk": resolve(root, "src/sdk/index.ts"),
    },
    dedupe: ["react", "react-dom"],
  },
  esbuild: { jsx: "automatic" },
  build: {
    // Outside the repository unless told otherwise.
    outDir: process.env.GALLERY_OUT ?? join(tmpdir(), "surface-gallery"),
    emptyOutDir: true,
    rollupOptions: { input: resolve(root, "gallery.html") },
    chunkSizeWarningLimit: 4000,
  },
  server: {
    host: process.env.GALLERY_HOST ?? "127.0.0.1",
    port: Number(process.env.GALLERY_PORT ?? 8430),
    strictPort: true,
    open: false,
    fs: { allow: [resolve(root, "..")] },
    // A preview is framed by a Surface app on another origin, and loaded over the network
    // when the reviewer is elsewhere: any host name the reviewer uses is accepted.
    allowedHosts: true,
  },
  appType: "mpa",
})

import { existsSync, writeFileSync } from "node:fs"
import { resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import { defineConfig } from "vite"

// One page source, many apps: SURFACE_APP picks the generated module list
// (src/generated/<app>.ts, written by `surface generate`), SURFACE_OUT where the build goes
// (the app's webui/dist, which its binary embeds).
const app = process.env.SURFACE_APP ?? "demo"
const root = import.meta.dirname
const generated = resolve(root, `src/generated/${app}.ts`)
if (!existsSync(generated)) {
  throw new Error(`no ${generated}: run \`go run ./cmd/surface generate -f examples/${app}/surface.yaml\` first`)
}
const outDir = process.env.SURFACE_OUT ?? resolve(root, `../examples/${app}/webui/dist`)
const api = process.env.SURFACE_API ?? "http://127.0.0.1:8190"

export default defineConfig({
  root,
  base: "/",
  plugins: [
    tailwindcss(),
    {
      // emptyOutDir removes dist/.gitkeep, which the Go embed needs to compile when unbuilt.
      name: "keep-gitkeep",
      closeBundle() {
        writeFileSync(resolve(outDir, ".gitkeep"), "")
      },
    },
  ],
  resolve: {
    alias: {
      "@": resolve(root, "src"),
      "@hollis-labs/surface-page/sdk": resolve(root, "src/sdk/index.ts"),
      "@hollis-labs/surface-theme": resolve(root, "../packages/theme/src/index.ts"),
      "@hollis-labs/surface-chat": resolve(root, "../packages/chat/src/index.ts"),
      "@hollis-labs/surface-cards": resolve(root, "../packages/cards/src/index.ts"),
      "@hollis-labs/surface-genui": resolve(root, "../packages/genui/src/index.ts"),
      "@surface/app": generated,
    },
    dedupe: ["react", "react-dom"],
  },
  esbuild: { jsx: "automatic" },
  build: { outDir, emptyOutDir: true, chunkSizeWarningLimit: 4000 },
  server: {
    fs: { allow: [resolve(root, "..")] },
    // ws: the Terminal's socket is under /api too.
    proxy: { "/api": { target: api, ws: true }, "/mcp": api },
  },
})

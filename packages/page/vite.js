// surfacePage(): the Vite settings an app needs to build with @hollis-labs/surface-page.
//
//   import { surfacePage } from "@hollis-labs/surface-page/vite"
//   export default defineConfig({ plugins: [tailwindcss(), surfacePage()], resolve: { alias: { "@": "src" } } })
//
// The page imports the app's own registry components (@/components/ui/*), so Vite must not
// pre-bundle it (that would put a second copy of those components inside the bundle). An excluded
// package's own imports are then pre-bundled here instead.
//
// Surface's own modules in an app outside the Surface repository: `surface generate` writes
// "@surface/modules/<id>" for their views. This plugin finds the Surface Go module the app
// requires (`go list -m`), resolves the specifier to modules/<id>/web/index.tsx there, and
// resolves those files' imports (react, @hollis-labs/*, lucide-react, ...) from the app, since
// the Go module cache has no node_modules. "@/..." already goes to the app's src (the alias above),
// so the modules draw with the app's registry components, as the page does.
//
// Tailwind must also see the classes those views use: this plugin writes
// <root>/src/generated/surface-modules.css with an @source line for the Surface modules on this
// machine. Import it from the app's CSS (`@import "./generated/surface-modules.css";`) and keep it
// out of git: the path differs per machine. Without Surface modules it is an empty comment.
import { execFileSync } from "node:child_process"
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs"
import { dirname, join, resolve, sep } from "node:path"

const SPECIFIER = "@surface/modules/"
const SURFACE_MODULE = "github.com/hollis-labs/surface"

/** The directory holding the app's go.mod, walking up from dir. */
function goRoot(dir) {
  let d = resolve(dir)
  for (;;) {
    if (existsSync(join(d, "go.mod"))) return d
    const up = dirname(d)
    if (up === d) return null
    d = up
  }
}

/** @returns {import("vite").Plugin} */
export function surfacePage() {
  let surfaceDir = null
  let appRoot = null
  const findSurface = () => {
    if (surfaceDir !== null) return surfaceDir
    const root = goRoot(appRoot ?? process.cwd())
    surfaceDir = ""
    if (!root) return surfaceDir
    try {
      surfaceDir = execFileSync("go", ["list", "-m", "-f", "{{.Dir}}", SURFACE_MODULE], {
        cwd: root,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      }).trim()
    } catch {
      surfaceDir = ""
    }
    return surfaceDir
  }
  return {
    name: "surface-page",
    enforce: "pre",
    config(config) {
      appRoot = config.root ? resolve(config.root) : process.cwd()
      const dir = findSurface()
      const css = join(appRoot, "src", "generated", "surface-modules.css")
      const body = dir
        ? `/* Written by surfacePage(); do not commit (the path is this machine's). */\n@source "${join(dir, "modules")}";\n`
        : "/* Written by surfacePage(): no Surface modules outside this repository. */\n"
      if (!existsSync(css) || readFileSync(css, "utf8") !== body) {
        mkdirSync(dirname(css), { recursive: true })
        writeFileSync(css, body)
      }
      return {
        optimizeDeps: {
          exclude: ["@hollis-labs/surface-page"],
          // What the page imports, pre-bundled for it (genui reaches ajv's CommonJS runtime).
          include: [
            "react",
            "react/jsx-runtime",
            "react-dom",
            "react-dom/client",
            "lucide-react",
            ...["surface-chat", "surface-cards", "surface-genui", "ade-core"].map(
              (p) => `@hollis-labs/surface-page > @hollis-labs/${p}`,
            ),
          ],
        },
        resolve: { dedupe: ["react", "react-dom"] },
        // Surface's modules (outside the app, with no tsconfig of their own beside them) use the
        // automatic JSX runtime, as the page does.
        esbuild: { jsx: "automatic" },
        // The dev server may read the Surface modules' views.
        ...(dir ? { server: { fs: { allow: [dir] } } } : {}),
      }
    },
    configResolved(config) {
      appRoot = config.root
      const aliased = config.resolve.alias.some((a) => a.find === "@" || String(a.find) === "/^@\\//")
      if (!aliased) {
        throw new Error(
          'surface-page: the page imports "@/components/ui/*" from the app. Set resolve.alias { "@": "<your src dir>" } in vite.config.',
        )
      }
    },
    async resolveId(source, importer, options) {
      if (source.startsWith(SPECIFIER)) {
        const dir = findSurface()
        if (!dir) {
          this.error(
            `surface-page: ${source}: cannot find ${SURFACE_MODULE} (go list -m from the app; is it in go.mod?)`,
          )
        }
        const id = source.slice(SPECIFIER.length)
        const entry = join(dir, "modules", id, "web", "index.tsx")
        if (!existsSync(entry)) this.error(`surface-page: ${source}: no ${entry}`)
        return entry
      }
      // A file of a Surface module (outside the app): resolve its packages from the app.
      const dir = findSurface()
      if (dir && importer && importer.startsWith(dir + sep) && !source.startsWith(".") && !source.startsWith("/")) {
        if (source.startsWith("@/")) return null // the app's alias handles it
        return this.resolve(source, join(appRoot, "index.html"), { ...options, skipSelf: true })
      }
      return null
    },
  }
}

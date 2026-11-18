// surfacePage(): the Vite settings an app needs to build with @hollis-labs/surface-page.
//
//   import { surfacePage } from "@hollis-labs/surface-page/vite"
//   export default defineConfig({ plugins: [tailwindcss(), surfacePage()], resolve: { alias: { "@": "src" } } })
//
// The page imports the app's own registry components (@/components/ui/*), so Vite must not
// pre-bundle it (that would put a second copy of those components inside the bundle). An excluded
// package's own imports are then pre-bundled here instead.

/** @returns {import("vite").Plugin} */
export function surfacePage() {
  return {
    name: "surface-page",
    config() {
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
      }
    },
    configResolved(config) {
      const aliased = config.resolve.alias.some((a) => a.find === "@" || String(a.find) === "/^@\\//")
      if (!aliased) {
        throw new Error(
          'surface-page: the page imports "@/components/ui/*" from the app. Set resolve.alias { "@": "<your src dir>" } in vite.config.',
        )
      }
    },
  }
}

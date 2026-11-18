#!/usr/bin/env node
// surface ui-shots: photograph the component gallery, every section in dark and in light.
//
// It starts the built app (bin/<app>) on a free loopback port with a throwaway data dir,
// opens the gallery as the only window with the agent panel closed, and saves one PNG per
// section and theme with headless Chrome. Nothing is compared yet: a later CI step diffs
// these against a reference.
//
//   node scripts/ui-shots.mjs [--app studio] [--out <dir>] [--width 1280]
//
// --out defaults to $TMPDIR/surface-ui-shots (outside the repository). CHROME names the
// browser when it is not in the usual place. Writes <out>/<theme>/<section>.png and
// <out>/index.json.
import { spawn } from "node:child_process"
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs"
import { createServer } from "node:net"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"

const args = process.argv.slice(2)
const opt = (name, fallback) => {
  const i = args.indexOf(`--${name}`)
  return i >= 0 ? args[i + 1] : fallback
}
const appName = opt("app", "studio")
const out = resolve(opt("out", join(tmpdir(), "surface-ui-shots")))
const width = Number(opt("width", "1280"))
const root = resolve(import.meta.dirname, "..")
const bin = join(root, "bin", appName)
const SECTIONS = ["controls", "display", "overlays", "chat", "cards", "genui", "ade", "windows"]
const THEMES = ["dark", "light"]
const env = appName.toUpperCase().replace(/-/g, "_")

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const die = (msg) => {
  console.error(`ui-shots: ${msg}`)
  process.exit(1)
}

function chromePath() {
  if (process.env.CHROME) return process.env.CHROME
  const mac = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  if (existsSync(mac)) return mac
  for (const p of ["/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"]) if (existsSync(p)) return p
  return die("no Chrome found; set CHROME")
}

const freePort = () =>
  new Promise((ok, fail) => {
    const s = createServer()
    s.once("error", fail)
    s.listen(0, "127.0.0.1", () => {
      const { port } = s.address()
      s.close(() => ok(port))
    })
  })

async function waitFor(url, tries = 80) {
  for (let i = 0; i < tries; i++) {
    try {
      const res = await fetch(url)
      if (res.ok) return res
    } catch {
      // not up yet
    }
    await sleep(150)
  }
  return die(`${url} did not answer`)
}

/** A small CDP client over the page target. */
async function openPage(port) {
  let targets
  for (let i = 0; i < 80 && !targets; i++) {
    try {
      targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json()
    } catch {
      await sleep(150)
    }
  }
  if (!targets) die("Chrome did not start")
  const ws = new WebSocket(targets.find((t) => t.type === "page").webSocketDebuggerUrl)
  await new Promise((r) => {
    ws.onopen = r
  })
  let id = 0
  const pending = new Map()
  ws.onmessage = (m) => {
    const d = JSON.parse(m.data)
    if (d.id && pending.has(d.id)) {
      pending.get(d.id)(d)
      pending.delete(d.id)
    }
    if (d.method === "Runtime.exceptionThrown") console.warn("page error:", d.params.exceptionDetails.text)
  }
  const send = (method, params = {}) =>
    new Promise((r) => {
      const i = ++id
      pending.set(i, r)
      ws.send(JSON.stringify({ id: i, method, params }))
    })
  const evaluate = async (expression) => {
    const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true })
    if (r.result?.exceptionDetails) throw new Error(r.result.exceptionDetails.exception?.description ?? "evaluate failed")
    return r.result?.result?.value
  }
  return { send, evaluate, close: () => ws.close() }
}

if (!existsSync(bin)) die(`${bin} is not built: run "make ${appName}" first`)
const data = mkdtempSync(join(tmpdir(), "surface-ui-shots-data-"))
const appPort = await freePort()
const base = `http://127.0.0.1:${appPort}`
const app = spawn(bin, ["serve"], {
  env: {
    ...process.env,
    [`${env}_DATA_DIR`]: data,
    [`${env}_ADDR`]: `127.0.0.1:${appPort}`,
    SURFACE_AGENT_CONFIG: join(data, "no-agent.json"),
  },
  stdio: "ignore",
})
const chromePort = await freePort()
const chrome = spawn(
  chromePath(),
  [
    "--headless=new",
    "--disable-gpu",
    "--hide-scrollbars",
    `--remote-debugging-port=${chromePort}`,
    `--user-data-dir=${join(data, "chrome")}`,
    "about:blank",
  ],
  { stdio: "ignore" },
)

const shots = []
try {
  await waitFor(`${base}/api/desk`)
  const post = (path, body, method = "POST") =>
    fetch(base + path, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
  await post("/api/tools/desk_arrange", { mode: "tile", windows: [{ view: "surface.gallery", focus: true }] })

  const page = await openPage(chromePort)
  await page.send("Page.enable")
  await page.send("Runtime.enable")
  const metrics = (height) =>
    page.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false })
  await metrics(900)
  await page.send("Page.navigate", { url: base })
  await sleep(1500)
  // The agent panel is closed, so the gallery has the width.
  await page.evaluate(`localStorage.setItem("surface.agent.open", "0"); location.reload(); 1`)
  await sleep(2500)

  for (const theme of THEMES) {
    await post("/api/settings/surface.theme", { value: theme }, "PUT")
    await sleep(500)
    mkdirSync(join(out, theme), { recursive: true })
    for (const section of SECTIONS) {
      await metrics(900)
      const found = await page.evaluate(`(async () => {
        const tab = document.querySelector('[data-gallery-tab="${section}"]')
        if (!tab) return false
        tab.click()
        await new Promise((r) => setTimeout(r, 400))
        return true
      })()`)
      if (!found) die(`the gallery has no section "${section}"`)
      // Make the window tall enough to hold the whole section, then photograph the section.
      const need = await page.evaluate(`(() => {
        const s = document.querySelector('[data-gallery-section="${section}"]')
        const scroller = s.parentElement
        return Math.ceil(scroller.getBoundingClientRect().top + s.scrollHeight + 40)
      })()`)
      await metrics(Math.min(Math.max(need, 600), 16000))
      await sleep(700)
      const clip = await page.evaluate(`(() => {
        const r = document.querySelector('[data-gallery-section="${section}"]').getBoundingClientRect()
        return { x: r.x, y: r.y, width: r.width, height: r.height, scale: 1 }
      })()`)
      const shot = await page.send("Page.captureScreenshot", { format: "png", clip, captureBeyondViewport: true })
      const file = join(out, theme, `${section}.png`)
      writeFileSync(file, Buffer.from(shot.result.data, "base64"))
      shots.push({ theme, section, file, width: Math.round(clip.width), height: Math.round(clip.height) })
      console.log(`${theme}/${section}.png  ${Math.round(clip.width)}×${Math.round(clip.height)}`)
    }
  }
  page.close()
  writeFileSync(join(out, "index.json"), `${JSON.stringify({ app: appName, width, shots }, null, 2)}\n`)
  console.log(`ui-shots: ${shots.length} shots in ${out}`)
} finally {
  chrome.kill()
  app.kill()
  await sleep(300)
  rmSync(data, { recursive: true, force: true })
}

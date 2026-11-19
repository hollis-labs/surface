#!/usr/bin/env node
// surface record-demo: run a script as a demo and record it (docs/scripts.md).
//
// It starts the built app (bin/<app>) on a free loopback port with a throwaway data dir,
// saves the script from a JSON file, opens the desk in headless Chrome, runs the script in
// demo mode and records the page with the DevTools screencast until the demo ends. The
// frames keep their timing; with ffmpeg on the PATH they become one MP4.
//
//   node scripts/record-demo.mjs --script tour.json [--app desk] [--out <dir>] [--width 1440 --height 900]
//
// --out defaults to $TMPDIR/surface-demo-<name>. CHROME names the browser when it is not in
// the usual place. Writes <out>/frames/*.jpg, <out>/frames.ffconcat, <out>/steps.json and,
// with ffmpeg, <out>/<name>.mp4.
//
// The app drives itself: every step's calls are tools, so this only watches. That is why the
// same script is also the onboarding guide and the test (script_run mode guide | test).
import { spawn, spawnSync } from "node:child_process"
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { createServer } from "node:net"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"

const args = process.argv.slice(2)
const opt = (name, fallback) => {
  const i = args.indexOf(`--${name}`)
  return i >= 0 ? args[i + 1] : fallback
}
const die = (msg) => {
  console.error(`record-demo: ${msg}`)
  process.exit(1)
}
const scriptFile = opt("script")
if (!scriptFile) die("give the script: --script <file.json> (as script_save takes it)")
const script = JSON.parse(readFileSync(resolve(scriptFile), "utf8"))
if (!script.name) die("the script has no name")
const appName = opt("app", "desk")
const width = Number(opt("width", "1440"))
const height = Number(opt("height", "900"))
const out = resolve(opt("out", join(tmpdir(), `surface-demo-${script.name}`)))
const root = resolve(import.meta.dirname, "..")
const bin = join(root, "bin", appName)
const env = appName.toUpperCase().replace(/-/g, "_")
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

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

/** A small CDP client over the page target, with events. */
async function openPage(port, onEvent) {
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
    } else if (d.method) onEvent(d.method, d.params)
  }
  const send = (method, params = {}) =>
    new Promise((r) => {
      const i = ++id
      pending.set(i, r)
      ws.send(JSON.stringify({ id: i, method, params }))
    })
  return { send, close: () => ws.close() }
}

/** Follows the app's event stream and resolves on the first event whose topic is wanted. */
async function until(base, topic, onEach) {
  const res = await fetch(`${base}/api/events`)
  const reader = res.body.getReader()
  const dec = new TextDecoder()
  let buf = ""
  for (;;) {
    const { value, done } = await reader.read()
    if (done) return null
    buf += dec.decode(value, { stream: true })
    let at
    while ((at = buf.indexOf("\n\n")) >= 0) {
      const block = buf.slice(0, at)
      buf = buf.slice(at + 2)
      // Every event comes as "surface", with {topic, data} in its data.
      const raw = /^data: (.*)$/m.exec(block)?.[1]
      if (!raw) continue
      let ev
      try {
        ev = JSON.parse(raw)
      } catch {
        continue
      }
      onEach?.(ev.topic, ev.data)
      if (ev.topic === topic) {
        reader.cancel().catch(() => {})
        return ev.data ?? null
      }
    }
  }
}

if (!existsSync(bin)) die(`${bin} is not built: run "make ${appName}" first`)
rmSync(out, { recursive: true, force: true })
mkdirSync(join(out, "frames"), { recursive: true })
const data = mkdtempSync(join(tmpdir(), "surface-demo-data-"))
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
    `--window-size=${width},${height}`,
    `--remote-debugging-port=${chromePort}`,
    `--user-data-dir=${join(data, "chrome")}`,
    "about:blank",
  ],
  { stdio: "ignore" },
)

const frames = []
const steps = []
try {
  await waitFor(`${base}/api/desk`)
  const saved = await fetch(`${base}/api/tools/script_save`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(script),
  })
  if (!saved.ok) die(`the script was not saved: ${await saved.text()}`)

  let page
  page = await openPage(chromePort, (method, params) => {
    if (method !== "Page.screencastFrame") return
    const n = String(frames.length).padStart(6, "0")
    const file = join(out, "frames", `${n}.jpg`)
    writeFileSync(file, Buffer.from(params.data, "base64"))
    frames.push({ file, t: params.metadata.timestamp })
    page?.send("Page.screencastFrameAck", { sessionId: params.sessionId })
  })
  await page.send("Page.enable")
  await page.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false })
  await page.send("Page.navigate", { url: base })
  await sleep(1500) // the desk draws
  await page.send("Page.startScreencast", { format: "jpeg", quality: 85, everyNthFrame: 1 })

  const ended = until(base, "script.done", (ev, raw) => {
    // raw is the event's data, already parsed
    if (ev === "script.step") steps.push({ at: Date.now(), ...raw })
  })
  const run = await fetch(`${base}/api/scripts/${encodeURIComponent(script.name)}/run`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ mode: "demo" }),
  })
  if (!run.ok) die(`the demo did not start: ${await run.text()}`)
  const report = await ended
  await sleep(800) // the last frame settles
  await page.send("Page.stopScreencast")
  page.close()
  writeFileSync(join(out, "steps.json"), JSON.stringify({ report, steps }, null, 2))

  // Each frame lasts until the next: the screencast sends frames only when something moves.
  if (frames.length === 0) die("no frames were recorded")
  let list = "ffconcat version 1.0\n"
  for (let i = 0; i < frames.length; i++) {
    const next = frames[i + 1]?.t ?? frames[i].t + 0.8
    list += `file '${frames[i].file}'\nduration ${Math.max(0.04, next - frames[i].t).toFixed(3)}\n`
  }
  list += `file '${frames.at(-1).file}'\n`
  const concat = join(out, "frames.ffconcat")
  writeFileSync(concat, list)
  const mp4 = join(out, `${script.name}.mp4`)
  const ff = spawnSync("ffmpeg", ["-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", concat, "-fps_mode", "vfr", "-pix_fmt", "yuv420p", mp4], { stdio: "inherit" })
  if (ff.error) console.log(`record-demo: ${frames.length} frames in ${out}/frames (no ffmpeg on the PATH for an MP4)`)
  else if (ff.status === 0) console.log(`record-demo: ${mp4}`)
  else die("ffmpeg failed")
  if (report?.status && report.status !== "done") die(`the demo ended ${report.status}`)
} finally {
  // Let both exit before their files go: Chrome writes its profile as it closes.
  const exited = (p) => new Promise((r) => (p.exitCode !== null ? r() : p.once("exit", r)))
  chrome.kill()
  app.kill()
  await Promise.race([Promise.all([exited(chrome), exited(app)]), sleep(5000)])
  try {
    rmSync(data, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 })
  } catch {
    console.warn(`record-demo: could not remove ${data}`)
  }
}

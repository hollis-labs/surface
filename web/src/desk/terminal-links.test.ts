import assert from "node:assert/strict"
import test from "node:test"
import { isWebURL, type LinkBuffer, type LinkLine, openWebLink, webLinksAt } from "../components/ui/terminal.links.ts"

/** A terminal buffer from lines of text: wrapped marks xterm's soft wrap; "＊" is a wide character. */
function buffer(cols: number, lines: { text: string; wrapped?: boolean }[]): LinkBuffer {
  const made: LinkLine[] = lines.map(({ text, wrapped }) => {
    const cells: { chars: string; width: number }[] = []
    for (const ch of text) {
      if (ch === "＊") {
        cells.push({ chars: ch, width: 2 }, { chars: "", width: 0 })
      } else cells.push({ chars: ch, width: 1 })
    }
    return {
      isWrapped: !!wrapped,
      getCell: (x) => {
        const c = cells[x]
        return c ? { getChars: () => c.chars, getWidth: () => c.width } : { getChars: () => "", getWidth: () => 1 }
      },
    }
  })
  return { cols, line: (y) => made[y] }
}

/** A long URL hard-wrapped at cols, as a CLI prints it (fold -w cols). */
function fold(s: string, cols: number): string[] {
  const out: string[] = []
  for (let i = 0; i < s.length; i += cols) out.push(s.slice(i, i + cols))
  return out
}

const LONG =
  "https://claude.ai/oauth/authorize?code=true&client_id=9d1c250a-e61b-44d9-88ed-5944d1962f5e&response_type=code" +
  "&redirect_uri=https%3A%2F%2Fconsole.anthropic.com%2Foauth%2Fcode%2Fcallback&scope=user%3Ainference" +
  "&code_challenge=AbCdEfGhIjKlMnOpQrStUvWxYz0123456789abcdefghi&code_challenge_method=S256&state=Zy"

test("a hard-wrapped URL is linked whole from every line it is on", () => {
  const cols = 60
  const parts = fold(LONG, cols)
  assert.ok(parts.length >= 5)
  const buf = buffer(cols, [
    { text: "Use the url below to sign in:" },
    ...parts.map((text) => ({ text })),
    { text: "Paste code here:" },
  ])
  for (let y = 2; y <= parts.length + 1; y++) {
    const links = webLinksAt(buf, y)
    assert.equal(links.length, 1, `line ${y}`)
    assert.equal(links[0].url, LONG)
    assert.deepEqual(links[0].range.start, { x: 1, y: 2 })
    assert.deepEqual(links[0].range.end, { x: parts.at(-1)?.length, y: parts.length + 1 })
  }
  assert.deepEqual(webLinksAt(buf, 1), [])
  assert.deepEqual(webLinksAt(buf, parts.length + 2), [])
})

test("the owner's case: 175 columns, a 376-character login URL, soft or hard", () => {
  const cols = 175
  const url = `${LONG}${"x".repeat(376 - LONG.length)}`
  const parts = fold(url, cols)
  const hard = buffer(
    cols,
    parts.map((text) => ({ text })),
  )
  const soft = buffer(
    cols,
    parts.map((text, i) => ({ text, wrapped: i > 0 })),
  )
  for (const buf of [hard, soft]) for (let y = 1; y <= parts.length; y++) assert.equal(webLinksAt(buf, y)[0]?.url, url)
})

test("an indented URL that wraps hard still joins; a short line ends it", () => {
  const cols = 40
  const lines = ["  https://example.com/a/very/long/path/x", "yz/more?q=1", "  next thing"]
  const buf = buffer(
    cols,
    lines.map((text) => ({ text })),
  )
  assert.equal(webLinksAt(buf, 2)[0]?.url, "https://example.com/a/very/long/path/xyz/more?q=1")
  assert.deepEqual(webLinksAt(buf, 3), [])
})

test("a line that does not reach the last column is not joined", () => {
  const buf = buffer(40, [{ text: "see https://example.com/docs" }, { text: "and more text" }])
  assert.deepEqual(
    webLinksAt(buf, 1).map((l) => l.url),
    ["https://example.com/docs"],
  )
  assert.deepEqual(webLinksAt(buf, 2), [])
})

test("columns follow wide characters, and trailing punctuation is not the URL", () => {
  const buf = buffer(40, [{ text: "＊＊ docs: https://example.com/a_(b)." }])
  const [link] = webLinksAt(buf, 1)
  assert.equal(link.url, "https://example.com/a_(b)")
  // Two wide characters take four columns: "＊＊ docs: " fills columns 1 to 11.
  assert.deepEqual(link.range.start, { x: 12, y: 1 })
  assert.deepEqual(link.range.end, { x: 12 + "https://example.com/a_(b)".length - 1, y: 1 })
  assert.equal(webLinksAt(buffer(40, [{ text: "(see https://example.com/x)" }]), 1)[0]?.url, "https://example.com/x")
})

test("only http and https open, with no opener and no referrer", () => {
  const opened: string[][] = []
  const open = (...a: string[]) => opened.push(a)
  assert.equal(openWebLink("https://example.com/x", open), true)
  assert.equal(openWebLink("javascript:alert(1)", open), false)
  assert.equal(openWebLink("file:///etc/passwd", open), false)
  assert.deepEqual(opened, [["https://example.com/x", "_blank", "noopener,noreferrer"]])
  assert.equal(isWebURL("http://"), false)
  assert.deepEqual(webLinksAt(buffer(40, [{ text: "ftp://example.com/file" }]), 1), [])
})

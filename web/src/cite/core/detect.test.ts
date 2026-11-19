// Detection fixtures, ported from Tachyon's console (plugins/console/ui/src/refs/detect.test.ts)
// by way of Basalisk, with Surface's kinds: relative paths are their own kind (path.relative),
// and the console's workstream and @agent kinds are not here.
import assert from "node:assert/strict"
import { describe, it } from "node:test"
import {
  codeRanges,
  createDefaultRefRegistry,
  createRefRegistry,
  detect,
  detectRefs,
  parseTachHref,
  uniqueRefs,
} from "./index.ts"
import type { RefKind } from "./types.ts"

const reg = createDefaultRefRegistry()
const refs = (text: string, o?: Parameters<typeof detectRefs>[2]) =>
  detectRefs(text, reg, o).map((r) => [r.kind, r.canonical])
const raws = (text: string) => detectRefs(text, reg).map((r) => r.raw)
const eq = assert.deepStrictEqual

describe("torque ids", () => {
  it("detects each entity type", () => {
    eq(refs("CW-20261016-0101 EP-20261016-0002 PRJ-20260416-0001 SP-20260101-0003 PL-20260202-0004"), [
      ["torque.task", "CW-20261016-0101"],
      ["torque.epic", "EP-20261016-0002"],
      ["torque.project", "PRJ-20260416-0001"],
      ["torque.sprint", "SP-20260101-0003"],
      ["torque.plan", "PL-20260202-0004"],
    ])
  })
  it("strips surrounding punctuation", () => {
    eq(raws('(see CW-20261016-0101), and "EP-20261016-0002". Done: PRJ-20260416-0001!'), [
      "CW-20261016-0101",
      "EP-20261016-0002",
      "PRJ-20260416-0001",
    ])
  })
  it("rejects malformed, embedded and file-name uses", () => {
    eq(refs("CW-2026101-0101 CW-20261016-01011 XCW-20261016-0101 CW-20261016-0101-foo cw-20261016-0101"), [])
    eq(refs("tasks/CW-20261016-0101"), [])
  })
  it("allows a sentence-ending period", () => {
    eq(raws("Fixed in CW-20261016-0101."), ["CW-20261016-0101"])
  })
})

describe("urls", () => {
  it("drops trailing punctuation and unbalanced closers", () => {
    eq(raws("see https://example.com/a/b."), ["https://example.com/a/b"])
    eq(raws("(https://example.com/a)"), ["https://example.com/a"])
    eq(raws("[docs](https://example.com/a_(b))"), ["https://example.com/a_(b)"])
    eq(raws("**https://example.com/x**"), ["https://example.com/x"])
    eq(raws("go to <https://example.com/x>, now"), ["https://example.com/x"])
  })
  it("keeps query and fragment", () => {
    eq(raws("https://example.com/a?b=1&c=2#top"), ["https://example.com/a?b=1&c=2#top"])
  })
  it("accepts localhost, ignores hostless schemes", () => {
    eq(raws("http://localhost:5178/x"), ["http://localhost:5178/x"])
    eq(refs("https://nohost http://"), [])
  })
  it("beats inner kinds: ids and paths inside a URL are not separate refs", () => {
    eq(refs("https://torque.example.com/tasks/CW-20261016-0101 and https://github.com/o/r/blob/main/src/a.ts"), [
      ["url", "https://torque.example.com/tasks/CW-20261016-0101"],
      ["url", "https://github.com/o/r/blob/main/src/a.ts"],
    ])
  })
})

describe("tesseract", () => {
  it("detects ULID item ids", () => {
    eq(refs("item 01M51F1RPQ5FD8MHP71TRSBN7H here"), [["tesseract.item", "01M51F1RPQ5FD8MHP71TRSBN7H"]])
    eq(refs("tesseract:01M51F1RPQ5FD8MHP71TRSBN7H"), [["tesseract.item", "01M51F1RPQ5FD8MHP71TRSBN7H"]])
  })
  it("rejects look-alikes", () => {
    eq(refs("ABCDEFGHJKMNPQRSTVWXYZABCD"), [])
    eq(refs("01M51F1RPQ5FD8MHP71TRSBN7HX"), [])
    eq(refs("01M51F1RPQ5FD8MHP71TRSBN7"), [])
    eq(refs("01m51f1rpq5fd8mhp71trsbn7h"), [])
    eq(refs("91M51F1RPQ5FD8MHP71TRSBN7H"), [])
  })
  it("detects explicit and bare keys", () => {
    eq(refs("tesseract:project/torque/workspace/handoff#agent_console_gui."), [
      ["tesseract.key", "project/torque/workspace/handoff#agent_console_gui"],
    ])
    eq(refs("tesseract:workspace@project/torque#k1"), [["tesseract.key", "workspace@project/torque#k1"]])
    eq(refs("see project/torque/workspace/handoff#my_key, ok"), [
      ["tesseract.key", "project/torque/workspace/handoff#my_key"],
    ])
  })
  it("does not treat C#, issue#12, or a bare anchor as a key", () => {
    eq(refs("C# issue#12 foo/bar#baz README.md#install"), [])
  })
})

describe("paths", () => {
  it("detects absolute, home and repo-relative paths with line numbers", () => {
    eq(refs("/srv/agentcow/worktrees/x and ~/dev/hollis-labs and apps/gui/src/main.tsx:42:7"), [
      ["path", "/srv/agentcow/worktrees/x"],
      ["path", "~/dev/hollis-labs"],
      ["path.relative", "apps/gui/src/main.tsx"],
    ])
    assert.equal(detectRefs("at src/a/b.ts:42:7", reg)[0].meta?.line, 42)
    assert.equal(detectRefs("at src/a/b.ts:42:7", reg)[0].meta?.col, 7)
  })
  it("detects dotted relatives and trailing-slash dirs", () => {
    eq(refs("../x/y.go ./cmd/server/ and docs/"), [
      ["path.relative", "../x/y.go"],
      ["path.relative", "./cmd/server"],
    ])
  })
  it("detects a path on a remote place", () => {
    eq(refs("see remote:box-dev/src/app/main.go:12."), [["path", "remote:box-dev/src/app/main.go"]])
  })
  it("drops trailing punctuation", () => {
    eq(raws("edit src/lib/api.ts, then (src/lib/types.ts)."), ["src/lib/api.ts", "src/lib/types.ts"])
  })
  it("rejects prose that merely contains slashes", () => {
    eq(refs("and/or either/or 10/16/2026 TCP/IP w/o 24/7 Q3/Q4 v1.2/3"), [])
    eq(refs("/api/planner/torque-titles /operations"), [])
  })
  it("rejects bare domains and versions", () => {
    eq(refs("github.com/hollis-labs/torque example.com/a/b.go 1.2.3/4.5"), [])
  })
  it("a task id inside a file name stays part of the path", () => {
    eq(refs("docs/CW-20261016-0101.md"), [["path.relative", "docs/CW-20261016-0101.md"]])
  })
})

describe("surface kinds", () => {
  const frag = "ab".repeat(32)
  it("fragments: explicit forms only, never a bare digest", () => {
    eq(refs(`tach:fragment/${frag} and https://fe.example.test/sysop/reader/${frag}`), [
      ["fragments.fragment", frag],
      ["fragments.fragment", frag],
    ])
    eq(refs(frag), [])
  })
  it("tether addresses and messages", () => {
    eq(refs("ask msg://agent/agent-mux/agt_example001."), [["tether.agent", "msg://agent/agent-mux/agt_example001"]])
    eq(refs("see tach:message/0B8C7A0E-1111-4222-8333-444455556666"), [
      ["tether.message", "0b8c7a0e-1111-4222-8333-444455556666"],
    ])
    eq(refs("0b8c7a0e-1111-4222-8333-444455556666"), [])
  })
  it("caster documents and work items", () => {
    eq(refs("in caster://w-portfolio/i.ab12cd34, see work:portfolio/ab12cd34."), [
      ["caster.doc", "caster://w-portfolio/i.ab12cd34"],
      ["work.item", "portfolio/ab12cd34"],
    ])
    eq(refs("work:portfolio/ab12cd345 work:Portfolio/ab12cd34"), [])
  })
  it("tach links name every service", () => {
    eq(parseTachHref("tach:torque/cw-20261019-0028"), { kind: "torque.task", canonical: "CW-20261019-0028" })
    eq(parseTachHref("tach:tesseract/01M4SCJHDM8FV6MDPSDTMZ0X44"), {
      kind: "tesseract.item",
      canonical: "01M4SCJHDM8FV6MDPSDTMZ0X44",
    })
    eq(parseTachHref("tach:tesseract/project/x/workspace/decisions#k"), {
      kind: "tesseract.key",
      canonical: "project/x/workspace/decisions#k",
    })
    eq(parseTachHref(`tach:fragment/${frag}`), { kind: "fragments.fragment", canonical: frag })
    eq(parseTachHref("tach:work/portfolio/ab12cd34"), { kind: "work.item", canonical: "portfolio/ab12cd34" })
    eq(parseTachHref("tach:caster/notes/plan"), { kind: "caster.doc", canonical: "caster://notes/plan" })
    assert.equal(parseTachHref("tach:nowhere/x"), null)
    assert.equal(parseTachHref("https://example.com"), null)
  })
})

describe("code spans", () => {
  it("skips fenced blocks by default", () => {
    const t = "before CW-20261016-0101\n```ts\nCW-20261016-0002 and src/a.ts\n```\nafter EP-20261016-0002"
    eq(refs(t), [
      ["torque.task", "CW-20261016-0101"],
      ["torque.epic", "EP-20261016-0002"],
    ])
  })
  it("skips inline code", () => {
    eq(refs("use `CW-20261016-0101` not CW-20261016-0102"), [["torque.task", "CW-20261016-0102"]])
    eq(refs("use ``a ` CW-20261016-0101`` ok"), [])
  })
  it("an unterminated fence runs to the end", () => {
    eq(refs("x\n```\nCW-20261016-0101"), [])
  })
  it("a longer closing fence closes; a shorter does not", () => {
    eq(refs("````\n```\nCW-20261016-0101\n````\nEP-20261016-0002"), [["torque.epic", "EP-20261016-0002"]])
    assert.equal(refs("~~~\nCW-20261016-0101\n~~~\nEP-20261016-0002").length, 1)
  })
  it("a lone backtick is literal", () => {
    eq(refs("a ` CW-20261016-0101"), [["torque.task", "CW-20261016-0101"]])
  })
  it("a ref straddling a span edge is dropped", () => {
    eq(refs("CW-2026`1016-0101` x"), [])
  })
  it("detects inside code when asked", () => {
    eq(refs("`CW-20261016-0101`", { skipCode: false }), [["torque.task", "CW-20261016-0101"]])
  })
  it("codeRanges are sorted and disjoint", () => {
    const r = codeRanges("`a` ```\nx\n``` `b`")
    for (let i = 1; i < r.length; i++) assert.ok(r[i][0] >= r[i - 1][1])
  })
})

describe("overlaps and priority", () => {
  const kind = (id: string, priority: number, word: string): RefKind => ({
    id,
    label: id,
    icon: "x",
    priority,
    scan: (t) => {
      const out = []
      let i = t.indexOf(word)
      while (i !== -1) {
        out.push({ start: i, end: i + word.length, canonical: word })
        i = t.indexOf(word, i + 1)
      }
      return out
    },
    open: () => ({ type: "none" }),
  })
  it("higher priority wins a contested span", () => {
    const r = createRefRegistry()
    r.register(kind("a.low", 1, "foo bar"))
    r.register(kind("a.high", 5, "bar baz"))
    eq(
      detectRefs("foo bar baz", r).map((x) => x.kind),
      ["a.high"],
    )
  })
  it("equal priority prefers the longer match", () => {
    const r = createRefRegistry()
    r.register(kind("a.short", 1, "bar"))
    r.register(kind("a.long", 1, "foo bar"))
    eq(
      detectRefs("foo bar", r).map((x) => x.kind),
      ["a.long"],
    )
  })
  it("keeps adjacent non-overlapping matches", () => {
    const r = createRefRegistry()
    r.register(kind("a.x", 1, "ab"))
    assert.equal(detectRefs("abab", r).length, 2)
  })
})

describe("segments", () => {
  it("reassemble to the input exactly", () => {
    const text = "Hi: CW-20261016-0101 → see `x` https://e.com/a. ✓ src/a.ts:3\n```\nz\n```\n"
    const segs = detect(text, reg)
    assert.equal(segs.map((s) => text.slice(s.start, s.end)).join(""), text)
    eq(
      segs.filter((s) => s.type === "ref").map((s) => s.type === "ref" && s.kind),
      ["torque.task", "url", "path.relative"],
    )
  })
  it("handles empty and ref-only text", () => {
    eq(detect("", reg), [])
    eq(
      detect("CW-20261016-0101", reg).map((s) => s.type),
      ["ref"],
    )
  })
  it("handles multibyte text without shifting offsets", () => {
    const text = "日本語 😀 CW-20261016-0101 😀"
    const [r] = detectRefs(text, reg)
    assert.equal(text.slice(r.start, r.end), "CW-20261016-0101")
  })
  it("uniqueRefs dedupes in first-seen order", () => {
    const segs = detect("CW-20261016-0101 EP-20261016-0002 CW-20261016-0101", reg)
    eq(
      uniqueRefs(segs).map((r) => r.canonical),
      ["CW-20261016-0101", "EP-20261016-0002"],
    )
  })
})

describe("robustness", () => {
  it("survives a throwing or malformed kind", () => {
    const r = createDefaultRefRegistry()
    r.register({
      id: "bad.throws",
      label: "x",
      icon: "x",
      priority: 1,
      scan: () => {
        throw new Error("boom")
      },
      open: () => ({ type: "none" }),
    })
    r.register({
      id: "bad.hits",
      label: "x",
      icon: "x",
      priority: 1,
      scan: () => [
        { start: 5, end: 2, canonical: "x" },
        { start: -1, end: 3, canonical: "x" },
        { start: 0, end: 99, canonical: "x" },
      ],
      open: () => ({ type: "none" }),
    })
    const errs: string[] = []
    assert.equal(detectRefs("CW-20261016-0101", r, { onError: (k) => errs.push(k) }).length, 1)
    assert.ok(errs.includes("bad.throws"))
    assert.equal(errs.filter((k) => k === "bad.hits").length, 3)
  })
  it("linear on pathological inputs", () => {
    const inputs = [
      "a/".repeat(50_000),
      `https://${"a.".repeat(50_000)}`,
      "`".repeat(50_000),
      "`a".repeat(30_000),
      `/home/${"x/".repeat(50_000)}:`,
      `tesseract:project/${"a/".repeat(50_000)}#`,
      "CW-20261016-".repeat(20_000),
      "```\n".repeat(20_000),
      `${"(".repeat(50_000)}https://e.com/${")".repeat(50_000)}`,
      `msg://agent/${"a/".repeat(50_000)}`,
      `remote:box/${"a/".repeat(50_000)}`,
    ]
    for (const input of inputs) {
      const t0 = performance.now()
      detect(input, reg)
      assert.ok(performance.now() - t0 < 2000, `slow on ${input.slice(0, 20)}`)
    }
  })
})

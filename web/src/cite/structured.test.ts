import assert from "node:assert/strict"
import { test } from "node:test"
import {
  type DocNode,
  fieldsOf,
  humanize,
  parseJSONDocument,
  splitFrontMatter,
  titleOf,
  toDocNode,
} from "./structured.ts"

const EVIDENCE = JSON.stringify(
  {
    status: "candidate evidence only; not approved migration membership",
    messages: [
      {
        id: "01a1f632-f45f-738b-b716-3f35d78f3ae8",
        subject: "0078 E exact 16 candidates",
        body: "Exact E candidate evidence.\n\n01KPKFTCDRBFMXNGHV2ZQY2GPV | (unkeyed) | signals=undetermined",
        sent_at: "2026-11-19T10:04:00Z",
      },
      { id: "m2", name: "Second", count: 3 },
      { other: true },
    ],
  },
  null,
  2,
)

test("parseJSONDocument takes objects and arrays only", () => {
  assert.ok(parseJSONDocument(EVIDENCE))
  assert.ok(parseJSONDocument(" [1, 2] "))
  assert.equal(parseJSONDocument("# Title\n\n{not json}"), undefined)
  assert.equal(parseJSONDocument('"a string"'), undefined)
  assert.equal(parseJSONDocument("42"), undefined)
  assert.equal(parseJSONDocument('{"cut": "sho'), undefined)
  assert.equal(parseJSONDocument("---\ntitle: x\n---\n# Doc"), undefined)
  assert.equal(parseJSONDocument(undefined), undefined)
})

test("an object becomes labeled fields, short ones first", () => {
  const node = toDocNode(parseJSONDocument(EVIDENCE))
  assert.equal(node.type, "fields")
  const fields = (node as Extract<DocNode, { type: "fields" }>).fields
  assert.deepEqual(
    fields.map((f) => [f.label, f.node.type]),
    [
      ["Status", "scalar"],
      ["Messages", "list"],
    ],
  )
})

test("a list of objects becomes sub-cards titled by a telling field", () => {
  const node = toDocNode(parseJSONDocument(EVIDENCE)) as Extract<DocNode, { type: "fields" }>
  const list = node.fields[1].node as Extract<DocNode, { type: "list" }>
  assert.equal(list.items.length, 3)
  const [first, second, third] = list.items
  assert.equal(first.title, "0078 E exact 16 candidates")
  assert.equal(first.subtitle, "01a1f632-f45f-738b-b716-3f35d78f3ae8")
  // The title and id are not repeated among the fields.
  assert.deepEqual(
    first.fields.map((f) => f.key),
    ["sent_at", "body"],
  )
  const sent = first.fields[0].node as Extract<DocNode, { type: "scalar" }>
  assert.equal(sent.iso, "2026-11-19T10:04:00Z")
  // A long string with line breaks is a block of text, not a one-line value.
  const body = first.fields[1].node
  assert.equal(body.type, "text")
  assert.equal(second.title, "Second")
  assert.equal(second.subtitle, "m2")
  assert.equal(third.title, "Item 3")
})

test("long strings read as Markdown only when they look like it", () => {
  const plain = toDocNode(`line one\nline two | a | b`)
  assert.deepEqual(plain, { type: "text", text: "line one\nline two | a | b", markdown: false })
  const md = toDocNode("# Heading\n\n- one\n- two")
  assert.equal(md.type === "text" && md.markdown, true)
})

test("scalars, lists of values, ids and depth", () => {
  assert.deepEqual(toDocNode(null), { type: "scalar", text: "none", empty: true })
  assert.deepEqual(toDocNode(true), { type: "scalar", text: "yes" })
  assert.deepEqual(toDocNode(["a", "b"]), { type: "values", values: ["a", "b"] })
  assert.equal((toDocNode("CW-20261119-0064") as { mono?: boolean }).mono, true)
  assert.equal((toDocNode("hello") as { mono?: boolean }).mono, false)
  let deep: unknown = { leaf: 1 }
  for (let i = 0; i < 8; i++) deep = { inner: deep }
  const text = JSON.stringify(toDocNode(deep))
  assert.match(text, /"type":"json"/)
})

test("labels and titles", () => {
  assert.equal(humanize("created_at"), "Created at")
  assert.equal(humanize("messageId"), "Message id")
  assert.deepEqual(titleOf({ id: "x" }, 0), { title: "x", used: ["id"] })
  assert.deepEqual(
    fieldsOf({ notes: "a\nb", n: 1 }).map((f) => f.key),
    ["n", "notes"],
  )
})

test("front matter is split off as fields", () => {
  const fm = splitFrontMatter("---\ntitle: Plan notes\nowner: 'chief'\ntags:\n  - a\n---\n# Plan\n\nBody")
  assert.ok(fm)
  assert.deepEqual(
    fm.fields.map((f) => [f.label, f.node.type === "scalar" && f.node.text]),
    [
      ["Title", "Plan notes"],
      ["Owner", "chief"],
    ],
  )
  assert.equal(fm.rest, "# Plan\n\nBody")
  assert.equal(splitFrontMatter("# No front matter\n---\n"), undefined)
})

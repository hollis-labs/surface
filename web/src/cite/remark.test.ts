import assert from "node:assert/strict"
import { describe, it } from "node:test"
import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { createDefaultRefRegistry } from "./core/index.ts"
import { CITE_ATTR, createRemarkCite } from "./remark.ts"

const remarkCite = createRemarkCite(createDefaultRefRegistry())

// The cite element drawn as [[kind id label mono]] so the markup says what the plugin made.
function render(md: string): string {
  return renderToStaticMarkup(
    createElement(
      ReactMarkdown,
      {
        remarkPlugins: [remarkGfm, remarkCite],
        components: {
          cite: ((p: Record<string, unknown>) =>
            `[[${p[CITE_ATTR.kind]} ${p[CITE_ATTR.id]}${p[CITE_ATTR.label] ? ` label=${p[CITE_ATTR.label]}` : ""}${p[CITE_ATTR.mono] ? " mono" : ""}]]`) as unknown as "cite",
        },
      },
      md,
    ),
  )
}

describe("remarkCite", () => {
  it("turns references in prose into cite elements", () => {
    assert.equal(
      render("Fixed in CW-20261119-0064, see ~/dev/app/main.go."),
      "<p>Fixed in [[torque.task CW-20261119-0064]], see [[path ~/dev/app/main.go]].</p>",
    )
  })
  it("leaves fenced code alone and takes inline code that is one reference", () => {
    const out = render("Run `CW-20261119-0064` and `make CW-20261119-0064`.\n\n```\nCW-20261119-0064\n```")
    assert.match(out, /Run \[\[torque\.task CW-20261119-0064 mono\]\] and <code>make CW-20261119-0064<\/code>/)
    assert.match(out, /<pre><code>CW-20261119-0064\n<\/code><\/pre>/)
  })
  it("makes a tach link a chip with its label", () => {
    assert.equal(
      render("[the design](tach:tesseract/01M7T1BJ53BCQWTPC844XEEN4Q)"),
      "<p>[[tesseract.item 01M7T1BJ53BCQWTPC844XEEN4Q label=the design]]</p>",
    )
  })
  it("makes a bare URL a chip, and keeps a labelled link a link", () => {
    assert.equal(render("see https://example.com/a."), "<p>see [[url https://example.com/a]].</p>")
    assert.equal(render("[docs](https://example.com/a)"), '<p><a href="https://example.com/a">docs</a></p>')
  })
  it("makes a fragment reader link a fragment chip", () => {
    const id = "ab".repeat(32)
    assert.equal(
      render(`[A thought](https://fe.example.test/sysop/reader/${id})`),
      `<p>[[fragments.fragment ${id} label=A thought]]</p>`,
    )
  })
  it("leaves the text of a link alone", () => {
    assert.equal(
      render("[CW-20261119-0064 notes](https://example.com/x)"),
      '<p><a href="https://example.com/x">CW-20261119-0064 notes</a></p>',
    )
  })
  it("works in lists, emphasis and tables", () => {
    const out = render("- **CW-20261119-0064**\n\n| a |\n|---|\n| work:portfolio/ab12cd34 |")
    assert.match(out, /<strong>\[\[torque\.task CW-20261119-0064\]\]<\/strong>/)
    assert.match(out, /<td>\[\[work\.item portfolio\/ab12cd34\]\]<\/td>/)
  })
})

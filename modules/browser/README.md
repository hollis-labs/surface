# Browser

Enter a URL or search words in the address bar. DuckDuckGo home and query URLs render a
local search view: the server parses its HTML endpoint into up to 15 results with titles,
URLs and snippets. Selecting a result uses the same window history as ordinary navigation.
Search failures are shown with the system-browser action still available; no search iframe
is loaded.

Reader mode selects a marked main/article region, or scores paragraph-rich containers
while penalizing link-heavy navigation. It renders Markdown through the shared registry
component, retaining heading levels, links, emphasis, lists and images. Scripts, forms,
navigation and source HTML are excluded. Links and image sources must resolve to HTTP(S)
URLs; images load lazily without a referrer. Content without a readable article shows an
explicit empty state. Search and reader requests do not use a browser session or cookie jar.

This is static HTML extraction: JavaScript-only pages still need the system browser, and
DuckDuckGo may rate-limit or challenge automated searches. Existing frame mode still honors
the fetched framing headers. A server-side proxied frame is a separate design-reviewed
phase; this module does not rewrite or proxy page scripts/assets.

Tests use fixture HTML and an injected HTTP transport; they do not depend on live search
service availability. Run `go test -race ./modules/browser` and the page's typecheck/lint.

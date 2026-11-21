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

## Fetch policy

Agent reads, link lists, searches and background context use a separate public-only
fetcher/cache. Each request (including redirects) resolves all addresses, rejects mixed
public/private answers and local interface networks, then dials a validated numeric address.
Host/SNI and ordinary TLS certificate verification retain the original hostname. Connections
are not reused across requests. The transport ignores environment proxies, cookies and
credentials. Requests are GET-only; DuckDuckGo uses its HTML endpoint's GET query.

The owner's Reader uses `POST /read` with the core's owner context; it intentionally permits
internal services. `GET /read`, outside-client POSTs and `public=true` always select the public
policy. Agent-opened windows carry `public_only` and use guarded Reader without direct frames
or image requests. Deliberate owner navigation returns to the owner policy. Owner and public
caches never mix. The existing local API actor/origin model is not authentication against a
hostile local process; this policy constrains Browser's agent tools and automatic fetches.

Both server paths reject credential-bearing URLs and add no cookie/authentication headers.
Limits for bytes (compressed and decoded), redirects, time and concurrency are centralized in
`limits.go`; public responses are limited to 2 MiB. Snapshot pipeline limits are declared there
for the next slice. Snapshot rendering itself is not part of this fetcher change.

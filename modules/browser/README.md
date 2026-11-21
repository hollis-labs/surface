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
the fetched framing headers. The styled snapshot option fetches static styling and raster images on the server; it never proxies scripts.

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
policy. Agent-opened windows carry `public_only` and use guarded snapshots or Reader without direct frames
or browser-originated image requests. Deliberate owner navigation returns to the owner policy. Owner and public
caches never mix. The existing local API actor/origin model is not authentication against a
hostile local process; this policy constrains Browser's agent tools and automatic fetches.

Both server paths reject credential-bearing URLs and add no cookie/authentication headers.
Limits for bytes (compressed and decoded), redirects, time and concurrency are centralized in
`limits.go`; public responses are limited to 2 MiB. Snapshot rendering uses the same public-only client for every document and asset.

## Styled snapshots

Pages that deny framing (and agent-opened pages) default to **Styled snapshot** unless the
Reader preference is enabled. The globe toggle also requests a snapshot for any page.
Reader remains available and is shown on snapshot failure. Snapshot link selections stay
public-only. Deliberately typing an address still permits the owner's ordinary internal
Reader/direct-frame access; snapshot fetching never permits internal targets.

`POST /snapshot?url=…` returns bounded JSON with sanitized HTML, plain text, title, links,
fetch timestamp and missing-asset/dropped-CSS counts. It is `no-store`; no snapshot bodies
are logged, written to disk or cached on the server. `browser_snapshot` exposes the text,
links and metadata to agents, without the HTML. It executes no script and opens no window.

HTML uses a static tag/attribute allowlist. Forms, controls, scripts, event handlers,
SVG/MathML, embedded frames/media, navigation, prefetch and original policy metadata are
removed. Links become a Surface-owned list outside the frame. The first valid base URL is
used only for resolution. Raster PNG/JPEG/GIF inputs are decoded, pixel-limited and
re-encoded as static PNG data URLs. Upstream data URLs and fonts are not accepted.

CSS uses `github.com/tdewolff/parse/v2`, not regular-expression rewriting. Common static
layout, typography and color declarations, media/supports/layer blocks and unqualified
imports are supported. Unsafe or unsupported declarations/rules are dropped individually;
the rest survives. Escaped identifiers, custom variables, unknown functions, animations,
font-face, selector functions and qualified imports are deliberately unsupported and
counted. Every supported image URL is resolved against that stylesheet's final URL and
rewritten; bounded imports detect cycles. Broken/oversized images become missing assets.
Stylesheets are serialized with a separate raw-text escaping boundary.

The parent renders only `srcDoc` in an iframe with `sandbox=""` and `referrerPolicy="no-referrer"`.
It grants neither scripts nor same-origin access. The first meta enforces CSP with default,
script, connect, frame, object, font and media sources all `none`; styles permit only inline
content and images only data URLs. No upstream page HTML enters the parent DOM. Scripts,
sign-in, forms, video, custom fonts and JavaScript-only apps will not work; the view explains
this and retains the system-browser action. Upstream servers see the server's address and
the supplied URL; a signed URL can itself convey access.

Budgets are centralized in `limits.go`: HTML2MiB, CSS512KiB, images2MiB/16MP (32MP total decoded pixels), total upstream
bytes12MiB (failed reads charged too), retained encoded assets12MiB, serialized HTML/JSON20MiB,
40 resources including the document, import depth2, 20,000 HTML tokens/DOM nodes, depth128,
100,000 CSS grammar steps and a 30-second snapshot deadline. Assets are fetched sequentially
(within the four-resource concurrency ceiling), limiting simultaneous raster decoding.
Two server reservations cover construction through response delivery; writes have a ten-second
deadline. These bound retained data and decoding work, not the Go allocator's exact RSS.
The page reserves at most two snapshot windows with responses capped at20MiB each; navigation,
Reader selection and close abort fetching and discard the body. No snapshot content goes into
persisted window args/state. Browser DOM/renderer memory is additional to the response budget.

Run `go test -race ./modules/browser`. For the opt-in real-Chrome isolation/network check:
`SURFACE_TEST_CHROME=/path/to/chrome go test ./modules/browser -run TestSnapshotBrowserIsolation -v`.
It uses a temporary profile and local fixture only, verifies the opaque frame and checks the
network log for zero upstream requests even when hostile content is added after sanitization.

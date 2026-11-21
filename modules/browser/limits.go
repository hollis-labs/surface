package browser

import "time"

// Shared fetch/snapshot budgets. Snapshot reservations are per application instance.
const (
	maxSnapshotPixels      = 32_000_000
	maxRetainedAssetBytes  = 12 << 20
	maxSnapshotConcurrency = 2
	maxCSSTokens           = 100_000
	maxSnapshotNodes       = 20_000
	maxSnapshotDepth       = 128
	maxSnapshotText        = 200_000
	maxSnapshotLinks       = 300
	maxPage                = 6 << 20 // legacy owner Reader
	maxHTML                = 2 << 20
	maxCSS                 = 512 << 10
	maxImage               = 2 << 20
	maxImagePixels         = 16_000_000
	maxSnapshotBytes       = 12 << 20
	maxSerializedBytes     = 20 << 20
	maxResources           = 40
	maxResourceConcurrency = 4
	maxImportDepth         = 2
	maxRedirects           = 3
	maxFetchConcurrency    = 8
	maxResponseHeaderBytes = 64 << 10
	maxCachedPages         = 200
	dnsTimeout             = 3 * time.Second
	connectTimeout         = 3 * time.Second
	resourceTimeout        = 10 * time.Second
	snapshotTimeout        = 30 * time.Second
	cacheTTL               = 5 * time.Minute
)

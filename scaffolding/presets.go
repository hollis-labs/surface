// Package scaffolding ships Surface-owned project presets for Substrate's Folio.
package scaffolding

import "embed"

// Presets is a trusted catalog mounted with folio.WithPresetPacks.
//
//go:embed all:presets
var Presets embed.FS

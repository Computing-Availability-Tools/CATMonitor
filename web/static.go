package main

import "embed"

// staticFiles embeds the frontend assets so the binary is self-contained and
// portable (no external web/static path lookup at runtime).
//
//go:embed static
var staticFiles embed.FS

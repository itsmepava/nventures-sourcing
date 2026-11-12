// Package web embeds the redesigned user interface: one HTML shell, local
// CSS and small native JavaScript modules. No CDN, fonts or analytics.
package web

import "embed"

// FS holds templates and static assets.
//
//go:embed templates static
var FS embed.FS

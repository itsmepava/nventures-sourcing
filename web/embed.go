// Package web embeds the separately developed frontend (web/src) into the
// Go binary so the workspace starts with one command. The frontend is plain
// HTML, CSS and native JavaScript modules that talk to the backend only
// through its JSON API (/api/...). During development, set NV_WEB_DIR=web/src
// to serve it from disk instead of the embedded copy.
package web

import "embed"

// FS holds the frontend under "src".
//
//go:embed src
var FS embed.FS

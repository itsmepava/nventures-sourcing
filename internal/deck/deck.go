// Package deck extracts plain text from pitch-deck files (PDF and PowerPoint
// PPTX) using only the Go standard library. It is deliberately bounded:
// input size, decompressed bytes, object counts, nesting depth and output
// text are all capped, and any parser failure becomes a status rather than
// a panic. Extracted text is untrusted data; nothing here interprets it.
//
// Not supported (reported, never guessed): encrypted/password-protected
// files, scanned decks without a text layer (no OCR), legacy binary .ppt,
// Keynote, and PDF streams using LZW or other rare filters.
package deck

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits.
const (
	MaxInputBytes       = 20 << 20  // largest file accepted
	maxDecodedPerStream = 16 << 20  // one decompressed stream
	maxDecodedTotal     = 96 << 20  // all decompressed data in one file
	maxObjects          = 200_000   // PDF indirect objects
	maxPages            = 400       // pages or slides read
	MaxTextRunes        = 300_000   // total extracted text kept
	maxNesting          = 64        // PDF array/dict nesting
	maxZipEntries       = 5_000     // PPTX archive entries
	maxContentTokens    = 5_000_000 // PDF content tokens interpreted per document
)

// Status values.
const (
	StatusExtracted   = "extracted"   // text was found
	StatusNoText      = "no_text"     // readable file, but no text layer (e.g. scanned images)
	StatusProtected   = "protected"   // encrypted / password-protected
	StatusUnsupported = "unsupported" // a format this package does not read
	StatusTooLarge    = "too_large"
	StatusError       = "error" // damaged or unparseable
)

// Result is the outcome of reading one deck.
type Result struct {
	Kind      string   `json:"kind"`   // pdf, pptx, or the detected/claimed type
	Status    string   `json:"status"` // see Status* constants
	Note      string   `json:"note,omitempty"`
	Pages     []string `json:"pages"`      // text per page/slide, in order
	PageCount int      `json:"page_count"` // pages/slides in the file (may exceed len(Pages) when capped)
	Truncated bool     `json:"truncated,omitempty"`
}

// Text joins the pages with page markers.
func (r Result) Text() string {
	var b strings.Builder
	for i, p := range r.Pages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p)
	}
	return b.String()
}

// Kind detects the file type from its content, falling back to the name.
func Kind(name, mime string, data []byte) string {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	switch {
	case bytes.Contains(head, []byte("%PDF-")):
		return "pdf"
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return "zip"
	case bytes.HasPrefix(data, []byte("\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1")):
		return "ole" // legacy Office binary, or an encrypted OOXML container
	}
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".key"):
		return "keynote"
	case strings.HasSuffix(n, ".ppt"):
		return "ppt"
	}
	return "unknown"
}

// Supported reports whether a filename/MIME type looks like a deck this
// package can attempt (used to decide whether to offer reading at all).
func Supported(name, mime string) bool {
	n, m := strings.ToLower(name), strings.ToLower(mime)
	return strings.HasSuffix(n, ".pdf") || strings.HasSuffix(n, ".pptx") ||
		m == "application/pdf" || m == "application/vnd.openxmlformats-officedocument.presentationml.presentation"
}

// recoverPanics is disabled by the fuzz test so parser bugs surface.
var recoverPanics = true

// Extract reads a deck. It never panics.
func Extract(name, mime string, data []byte) (res Result) {
	defer func() {
		if !recoverPanics {
			return
		}
		if p := recover(); p != nil {
			res = Result{Kind: res.Kind, Status: StatusError, Note: "The file could not be parsed (damaged or unusual structure)."}
		}
	}()
	if len(data) > MaxInputBytes {
		return Result{Kind: "unknown", Status: StatusTooLarge, Note: "Files over 20 MB are not read. Open the original instead."}
	}
	switch k := Kind(name, mime, data); k {
	case "pdf":
		res = readPDF(data)
	case "zip":
		res = readPPTX(data)
	case "ole":
		res = Result{Kind: "ole", Status: StatusProtected, Note: "This is a password-protected Office file or a legacy .ppt; it cannot be read. Open the original or ask for a PDF."}
		if strings.HasSuffix(strings.ToLower(name), ".ppt") {
			res.Kind, res.Status, res.Note = "ppt", StatusUnsupported, "Legacy .ppt files are not read. Open the original or ask for a PDF/PPTX."
		}
	case "keynote":
		res = Result{Kind: k, Status: StatusUnsupported, Note: "Keynote files are not read. Ask the founder for a PDF."}
	default:
		res = Result{Kind: k, Status: StatusUnsupported, Note: "Only PDF and PPTX decks are read."}
	}
	res.finish()
	return res
}

// finish normalises page text, applies the total text cap and sets status.
func (r *Result) finish() {
	if r.Status != "" && r.Status != StatusExtracted {
		r.Pages = nil
		return
	}
	budget := MaxTextRunes
	any := false
	for i, p := range r.Pages {
		p = tidy(p)
		if n := utf8.RuneCountInString(p); n > budget {
			p = string([]rune(p)[:budget])
			r.Truncated = true
			r.Pages = r.Pages[:i+1]
			r.Pages[i] = p
			any = any || strings.TrimSpace(p) != ""
			break
		} else {
			budget -= n
		}
		r.Pages[i] = p
		if strings.TrimSpace(p) != "" {
			any = true
		}
	}
	if r.PageCount < len(r.Pages) {
		r.PageCount = len(r.Pages)
	}
	if r.Pages == nil {
		r.Pages = []string{}
	}
	if any {
		r.Status = StatusExtracted
		if r.Truncated && r.Note == "" {
			r.Note = "Only the first part of the deck's text was kept."
		}
	} else {
		r.Status, r.Pages = StatusNoText, []string{}
		if r.Note == "" {
			r.Note = "No text layer was found. The deck is probably made of images (scanned or exported as pictures); open the original."
		}
	}
}

// tidy collapses runs of spaces, trims lines, drops control characters and
// limits blank lines.
func tidy(s string) string {
	s = strings.ToValidUTF8(s, "")
	var out []string
	blank := 0
	for _, line := range strings.Split(s, "\n") {
		line = strings.Map(func(r rune) rune {
			switch {
			case r == '\t' || r == '\u00a0':
				return ' '
			case r == '\ufffd' || r == '\ufeff' || r == '\u00ad':
				return -1
			case unicode.IsControl(r):
				return -1
			}
			return r
		}, line)
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

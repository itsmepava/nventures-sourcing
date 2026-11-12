// Package extract turns untrusted email and web content into reviewable,
// evidence-backed fields using deterministic parsing only. Text found in
// content is data: it is never interpreted as an instruction.
package extract

import (
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Link is an anchor found in a document.
type Link struct {
	URL  string `json:"url"`
	Text string `json:"text"`
}

// Document is a parsed HTML page.
type Document struct {
	Title       string
	Description string
	Text        string // block-separated visible text
	Links       []Link
	JSONLD      []map[string]any
	Feeds       []string
	Scripts     int
	NoScriptMsg bool
	Canonical   string
}

var blockAtoms = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Br: true, atom.Li: true, atom.Tr: true, atom.H1: true, atom.H2: true,
	atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true, atom.Section: true, atom.Article: true,
	atom.Header: true, atom.Footer: true, atom.Table: true, atom.Ul: true, atom.Ol: true, atom.Blockquote: true,
	atom.Td: true, atom.Th: true, atom.Address: true, atom.Main: true, atom.Nav: true, atom.Dd: true, atom.Dt: true,
}

var spaceRe = regexp.MustCompile(`[ \t\f\r\x{00a0}]+`)
var blankLinesRe = regexp.MustCompile(`\n\s*\n+`)

// ParseHTML parses a bounded HTML document. base resolves relative links.
func ParseHTML(r io.Reader, base *url.URL) (*Document, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	doc := &Document{}
	var text strings.Builder
	var walk func(n *html.Node, hidden bool)
	walk = func(n *html.Node, hidden bool) {
		switch n.Type {
		case html.TextNode:
			if !hidden {
				text.WriteString(n.Data)
			}
			return
		case html.ElementNode:
			switch n.DataAtom {
			case atom.Script:
				typ := strings.ToLower(attr(n, "type"))
				if typ == "application/ld+json" && n.FirstChild != nil {
					doc.JSONLD = append(doc.JSONLD, parseJSONLD(n.FirstChild.Data)...)
				} else {
					doc.Scripts++
				}
				return
			case atom.Style, atom.Template, atom.Svg, atom.Iframe, atom.Object:
				return
			case atom.Noscript:
				var b strings.Builder
				collectText(n, &b)
				if regexp.MustCompile(`(?i)enable javascript|requires javascript|javascript is (disabled|required)`).MatchString(b.String()) {
					doc.NoScriptMsg = true
				}
				return
			case atom.Title:
				if doc.Title == "" {
					var b strings.Builder
					collectText(n, &b)
					doc.Title = clean(b.String())
				}
				return
			case atom.Meta:
				name := strings.ToLower(attr(n, "name") + attr(n, "property"))
				if (name == "description" || name == "og:description") && doc.Description == "" {
					doc.Description = clean(attr(n, "content"))
				}
			case atom.Link:
				rel := strings.ToLower(attr(n, "rel"))
				typ := strings.ToLower(attr(n, "type"))
				href := resolve(base, attr(n, "href"))
				if rel == "canonical" && href != "" {
					doc.Canonical = href
				}
				if rel == "alternate" && (strings.Contains(typ, "rss") || strings.Contains(typ, "atom")) && href != "" {
					doc.Feeds = append(doc.Feeds, href)
				}
			case atom.A:
				if href := resolve(base, attr(n, "href")); href != "" {
					var b strings.Builder
					collectText(n, &b)
					label := clean(b.String())
					if label == "" {
						label = clean(attr(n, "aria-label") + " " + attr(n, "title"))
					}
					if label == "" {
						label = imgAlt(n)
					}
					doc.Links = append(doc.Links, Link{URL: href, Text: label})
				}
			}
			if attr(n, "hidden") != "" || strings.Contains(strings.ReplaceAll(strings.ToLower(attr(n, "style")), " ", ""), "display:none") {
				hidden = true
			}
		}
		block := n.Type == html.ElementNode && blockAtoms[n.DataAtom]
		if block {
			text.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, hidden)
		}
		if block {
			text.WriteString("\n")
		}
	}
	walk(root, false)
	doc.Text = normalizeBlocks(text.String())
	return doc, nil
}

// HTMLToText converts an HTML email body to readable text.
func HTMLToText(s string) string {
	d, err := ParseHTML(strings.NewReader(s), nil)
	if err != nil {
		return ""
	}
	return d.Text
}

func normalizeBlocks(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRe.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(blankLinesRe.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

func clean(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(strings.ReplaceAll(s, "\n", " "), " "))
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func collectText(n *html.Node, b *strings.Builder) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
			b.WriteString(" ")
		} else if c.Type == html.ElementNode && c.DataAtom != atom.Script && c.DataAtom != atom.Style {
			collectText(c, b)
		}
	}
}

func imgAlt(n *html.Node) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Img {
			if a := clean(attr(c, "alt")); a != "" {
				return a
			}
		}
		if a := imgAlt(c); a != "" {
			return a
		}
	}
	return ""
}

func resolve(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto" {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func parseJSONLD(raw string) []map[string]any {
	var v any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &v) != nil {
		return nil
	}
	var out []map[string]any
	var flatten func(v any)
	flatten = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, x := range t {
				flatten(x)
			}
		case map[string]any:
			if g, ok := t["@graph"]; ok {
				flatten(g)
			}
			out = append(out, t)
		}
	}
	flatten(v)
	return out
}

// JSONString reads a string or the first string of an array from a JSON-LD node.
func JSONString(m map[string]any, key string) string {
	switch t := m[key].(type) {
	case string:
		return clean(t)
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				return clean(s)
			}
		}
	case map[string]any:
		if s, ok := t["name"].(string); ok {
			return clean(s)
		}
	}
	return ""
}

// JSONStrings returns all string values for key.
func JSONStrings(m map[string]any, key string) []string {
	switch t := m[key].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

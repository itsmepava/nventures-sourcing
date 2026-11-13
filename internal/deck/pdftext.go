package deck

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf16"
)

// ---- fonts ----

type font struct {
	twoByte  bool           // composite (Type0) font: 2-byte codes
	toUni    map[int]string // ToUnicode CMap
	enc      *[256]rune     // simple-font encoding
	ucs2     bool           // Type0 with a Unicode encoding: code == UTF-16 unit
	widths   map[int]float64
	defWidth float64
}

type glyph struct {
	text  string
	code  int
	width float64 // in 1/1000 em
	space bool    // single-byte code 32 (word spacing applies)
}

func (f *font) decode(s []byte) []glyph {
	var out []glyph
	step := 1
	if f.twoByte {
		step = 2
	}
	for i := 0; i < len(s); i += step {
		code := int(s[i])
		if f.twoByte {
			if i+1 >= len(s) {
				break
			}
			code = int(s[i])<<8 | int(s[i+1])
		}
		g := glyph{code: code, space: !f.twoByte && code == 32}
		if t, ok := f.toUni[code]; ok {
			g.text = t
		} else if f.twoByte {
			if f.ucs2 {
				g.text = string(rune(code))
			}
		} else if f.enc != nil {
			if r := f.enc[code]; r != 0 {
				g.text = string(r)
			}
		}
		if w, ok := f.widths[code]; ok {
			g.width = w
		} else {
			g.width = f.defWidth
			if g.space || g.text == " " {
				g.width = 250
			}
		}
		out = append(out, g)
	}
	return out
}

func (d *pdfDoc) loadFont(v any) *font {
	fd := d.dict(v)
	f := &font{widths: map[int]float64{}, defWidth: 500}
	if fd == nil {
		f.enc = &winAnsi
		return f
	}
	sub, _ := d.resolve(fd["Subtype"]).(pName)
	if s, ok := d.resolve(fd["ToUnicode"]).(*pStream); ok {
		if data, err := d.decode(s); err == nil {
			f.toUni = parseCMap(data)
		}
	}
	if sub == "Type0" {
		f.twoByte = true
		f.defWidth = 1000
		if e, ok := d.resolve(fd["Encoding"]).(pName); ok && (strings.Contains(string(e), "UCS2") || strings.Contains(string(e), "UTF16")) {
			f.ucs2 = true
		}
		if desc := d.array(fd["DescendantFonts"]); len(desc) > 0 {
			cid := d.dict(desc[0])
			f.defWidth = d.num(cid["DW"], 1000)
			w := d.array(cid["W"])
			for i := 0; i < len(w) && len(f.widths) < 70000; {
				first, ok := d.resolve(w[i]).(float64)
				if !ok || i+1 >= len(w) {
					break
				}
				if arr, ok := d.resolve(w[i+1]).(pArray); ok {
					for k, x := range arr {
						f.widths[clampInt(first, 0, 1<<24)+k] = d.num(x, f.defWidth)
					}
					i += 2
					continue
				}
				if i+2 >= len(w) {
					break
				}
				lo, hi := clampInt(first, 0, 1<<24), clampInt(d.num(w[i+1], first), 0, 1<<24)
				wd := d.num(w[i+2], f.defWidth)
				for c := lo; c <= hi && c-lo < 70000; c++ {
					f.widths[c] = wd
				}
				i += 3
			}
		}
		return f
	}
	// Simple font (Type1, TrueType, Type3, MMType1).
	enc := winAnsi
	switch e := d.resolve(fd["Encoding"]).(type) {
	case pName:
		if e == "MacRomanEncoding" {
			enc = macRoman()
		}
	case pDict:
		if b, ok := d.resolve(e["BaseEncoding"]).(pName); ok && b == "MacRomanEncoding" {
			enc = macRoman()
		}
		code := 0
		for _, x := range d.array(e["Differences"]) {
			switch t := d.resolve(x).(type) {
			case float64:
				code = clampInt(t, -1, 256)
			case pName:
				if code >= 0 && code < 256 {
					if r := glyphRune(string(t)); r != 0 {
						enc[code] = r
					}
				}
				code++
			}
		}
	}
	f.enc = &enc
	first := clampInt(d.num(fd["FirstChar"], 0), 0, 1<<16)
	for i, x := range d.array(fd["Widths"]) {
		f.widths[first+i] = d.num(x, 0)
	}
	if sub == "Type3" {
		// Type 3 widths are in glyph space; scale by the font matrix.
		if m := d.array(fd["FontMatrix"]); len(m) == 6 {
			sc := d.num(m[0], 0.001) * 1000
			for k, w := range f.widths {
				f.widths[k] = w * sc
			}
		}
	}
	return f
}

// parseCMap reads bfchar/bfrange mappings from a ToUnicode CMap.
func parseCMap(data []byte) map[int]string {
	m := map[int]string{}
	l := &lexer{b: data, noRefs: true}
	var stack []any
	work := 400_000 // mapping entries written, including overwrites
	for len(m) < 200_000 && work > 0 {
		v, err := l.value(0)
		if err != nil {
			break
		}
		kw, ok := v.(pKw)
		if !ok {
			stack = append(stack, v)
			if len(stack) > 64 {
				stack = stack[1:]
			}
			continue
		}
		stack = stack[:0]
		switch kw {
		case "beginbfchar":
			for {
				a, err := l.value(0)
				if err != nil {
					return m
				}
				if k, ok := a.(pKw); ok && k == "endbfchar" {
					break
				}
				b, _ := l.value(0)
				src, ok1 := a.(pString)
				dst, ok2 := b.(pString)
				if ok1 && ok2 {
					m[bytesInt(src)] = utf16BE(dst)
					if work--; work <= 0 {
						return m
					}
				}
			}
		case "beginbfrange":
			for {
				a, err := l.value(0)
				if err != nil {
					return m
				}
				if k, ok := a.(pKw); ok && k == "endbfrange" {
					break
				}
				b, _ := l.value(0)
				c, _ := l.value(0)
				lo, ok1 := a.(pString)
				hi, ok2 := b.(pString)
				if !ok1 || !ok2 {
					continue
				}
				from, to := bytesInt(lo), bytesInt(hi)
				if to < from || to-from > 65535 {
					continue
				}
				if work -= to - from + 1; work <= 0 {
					return m
				}
				switch dst := c.(type) {
				case pString:
					u := utf16.Decode(units(dst))
					for code := from; code <= to; code++ {
						r := append([]rune(nil), u...)
						if len(r) > 0 {
							r[len(r)-1] += rune(code - from)
						}
						m[code] = string(r)
					}
				case pArray:
					for k, x := range dst {
						if s, ok := x.(pString); ok && from+k <= to {
							m[from+k] = utf16BE(s)
						}
					}
				}
			}
		}
	}
	return m
}

func bytesInt(b []byte) int {
	n := 0
	for _, c := range b {
		n = n<<8 | int(c)
	}
	return n
}

func units(b []byte) []uint16 {
	var u []uint16
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
	}
	if len(b)%2 == 1 { // some writers emit single bytes
		u = append(u, uint16(b[len(b)-1]))
	}
	return u
}

func utf16BE(b []byte) string { return string(utf16.Decode(units(b))) }

// ---- content streams ----

type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

// mul returns m × n (PDF row-vector convention).
func (m matrix) mul(n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

type textState struct {
	font                 *font
	size, tc, tw, th, tl float64
	rise                 float64
}

type extractor struct {
	d      *pdfDoc
	fonts  map[any]*font
	out    strings.Builder
	work   *int // tokens left for the whole document
	have   bool
	lastX  float64 // device x where the previous glyph run ended
	lastY  float64
	lastSz float64
	forms  map[int]bool
}

func (e *extractor) emit(s string) {
	if e.out.Len() < 4*MaxTextRunes {
		e.out.WriteString(s)
	}
}

func (e *extractor) endsWithBreak() bool {
	s := e.out.String()
	return s == "" || strings.HasSuffix(s, " ") || strings.HasSuffix(s, "\n")
}

func (e *extractor) fontFor(res pDict, name pName) *font {
	fonts := e.d.dict(res["Font"])
	ref := fonts[name]
	var key any = fmt.Sprintf("%p/%s", fonts, name) // inline font dict: per resource dict
	if r, ok := ref.(pRef); ok {
		key = r
	}
	if f, ok := e.fonts[key]; ok {
		return f
	}
	f := e.d.loadFont(ref)
	if len(e.fonts) < 10_000 {
		e.fonts[key] = f
	}
	return f
}

// show places a glyph string, inserting spaces/newlines from geometry.
func (e *extractor) show(s pString, ts *textState, tm *matrix, ctm matrix) {
	if ts.font == nil {
		ts.font = &font{enc: &winAnsi, widths: map[int]float64{}, defWidth: 500}
	}
	trm := matrix{ts.size * ts.th, 0, 0, ts.size, 0, ts.rise}.mul(*tm).mul(ctm)
	x, y := trm[4], trm[5]
	sz := math.Hypot(trm[2], trm[3])
	if sz < 0.5 {
		sz = math.Max(0.5, math.Abs(ts.size))
	}
	if e.have {
		ref := math.Max(sz, e.lastSz)
		switch {
		case math.Abs(y-e.lastY) > 0.5*ref:
			if !strings.HasSuffix(e.out.String(), "\n") {
				e.emit("\n")
			}
		case x-e.lastX > 0.15*ref || x-e.lastX < -1.5*ref:
			if !e.endsWithBreak() {
				e.emit(" ")
			}
		}
	}
	for _, g := range ts.font.decode(s) {
		if g.text != "" {
			e.emit(g.text)
		}
		adv := g.width/1000*ts.size + ts.tc
		if g.space {
			adv += ts.tw
		}
		*tm = matrix{1, 0, 0, 1, adv * ts.th, 0}.mul(*tm)
	}
	end := matrix{ts.size * ts.th, 0, 0, ts.size, 0, ts.rise}.mul(*tm).mul(ctm)
	e.lastX, e.lastY, e.lastSz, e.have = end[4], end[5], sz, true
}

func nums(st []any, n int) ([]float64, bool) {
	if len(st) < n {
		return nil, false
	}
	out := make([]float64, n)
	for i, v := range st[len(st)-n:] {
		f, ok := v.(float64)
		if !ok {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

// run interprets one content stream with its resources.
func (e *extractor) run(content []byte, res pDict, ctm matrix, depth int) {
	l := &lexer{b: content, noRefs: true}
	ts := textState{th: 1}
	tm, tlm := identity, identity
	var gstack []matrix
	var st []any
	for *e.work > 0 {
		v, err := l.value(0)
		if err != nil {
			return
		}
		*e.work--
		op, ok := v.(pKw)
		if !ok {
			st = append(st, v)
			if len(st) > 1024 {
				st = st[len(st)-64:]
			}
			continue
		}
		switch op {
		case "q":
			if len(gstack) < 256 {
				gstack = append(gstack, ctm)
			}
		case "Q":
			if n := len(gstack); n > 0 {
				ctm, gstack = gstack[n-1], gstack[:n-1]
			}
		case "cm":
			if f, ok := nums(st, 6); ok {
				ctm = matrix{f[0], f[1], f[2], f[3], f[4], f[5]}.mul(ctm)
			}
		case "BT":
			tm, tlm = identity, identity
		case "ET":
		case "Tf":
			if len(st) >= 2 {
				if n, ok := st[len(st)-2].(pName); ok {
					ts.font = e.fontFor(res, n)
				}
				if f, ok := st[len(st)-1].(float64); ok {
					ts.size = f
				}
			}
		case "Tc":
			if f, ok := nums(st, 1); ok {
				ts.tc = f[0]
			}
		case "Tw":
			if f, ok := nums(st, 1); ok {
				ts.tw = f[0]
			}
		case "Tz":
			if f, ok := nums(st, 1); ok {
				ts.th = f[0] / 100
			}
		case "TL":
			if f, ok := nums(st, 1); ok {
				ts.tl = f[0]
			}
		case "Ts":
			if f, ok := nums(st, 1); ok {
				ts.rise = f[0]
			}
		case "Td", "TD":
			if f, ok := nums(st, 2); ok {
				tlm = matrix{1, 0, 0, 1, f[0], f[1]}.mul(tlm)
				tm = tlm
				if op == "TD" {
					ts.tl = -f[1]
				}
			}
		case "Tm":
			if f, ok := nums(st, 6); ok {
				tlm = matrix{f[0], f[1], f[2], f[3], f[4], f[5]}
				tm = tlm
			}
		case "T*":
			tlm = matrix{1, 0, 0, 1, 0, -ts.tl}.mul(tlm)
			tm = tlm
		case "Tj":
			if len(st) > 0 {
				if s, ok := st[len(st)-1].(pString); ok {
					e.show(s, &ts, &tm, ctm)
				}
			}
		case "'", "\"":
			if op == "\"" {
				if f, ok := nums(st[:max(0, len(st)-1)], 2); ok {
					ts.tw, ts.tc = f[0], f[1]
				}
			}
			tlm = matrix{1, 0, 0, 1, 0, -ts.tl}.mul(tlm)
			tm = tlm
			if len(st) > 0 {
				if s, ok := st[len(st)-1].(pString); ok {
					e.show(s, &ts, &tm, ctm)
				}
			}
		case "TJ":
			if len(st) > 0 {
				if arr, ok := st[len(st)-1].(pArray); ok {
					for _, x := range arr {
						switch t := x.(type) {
						case pString:
							e.show(t, &ts, &tm, ctm)
						case float64:
							tm = matrix{1, 0, 0, 1, -t / 1000 * ts.size * ts.th, 0}.mul(tm)
						}
					}
				}
			}
		case "Do":
			if len(st) > 0 && depth < 8 {
				if n, ok := st[len(st)-1].(pName); ok {
					e.form(res, n, ctm, depth)
				}
			}
		case "BI": // skip inline image data up to EI
			if i := inlineImageEnd(content, l.pos); i > 0 {
				l.pos = i
			} else {
				return
			}
		}
		st = st[:0]
	}
}

func (e *extractor) form(res pDict, name pName, ctm matrix, depth int) {
	xo := e.d.dict(res["XObject"])
	ref, isRef := xo[name].(pRef)
	if isRef {
		if e.forms[ref.num] {
			return
		}
		e.forms[ref.num] = true
		defer delete(e.forms, ref.num)
	}
	s, ok := e.d.resolve(xo[name]).(*pStream)
	if !ok || s.dict["Subtype"] != pName("Form") {
		return
	}
	data, err := e.d.decode(s)
	if err != nil {
		return
	}
	if m := e.d.array(s.dict["Matrix"]); len(m) == 6 {
		var mm matrix
		for i := range mm {
			mm[i] = e.d.num(m[i], identity[i])
		}
		ctm = mm.mul(ctm)
	}
	fres := e.d.dict(s.dict["Resources"])
	if fres == nil {
		fres = res
	}
	e.run(data, fres, ctm, depth+1)
}

func inlineImageEnd(b []byte, from int) int {
	for i := from; i+2 <= len(b); {
		j := bytes.Index(b[i:], []byte("EI"))
		if j < 0 {
			return -1
		}
		k := i + j
		if k > 0 && isWS(b[k-1]) && (k+2 == len(b) || isWS(b[k+2])) {
			return k + 2
		}
		i = k + 2
	}
	return -1
}

// ---- pages ----

func readPDF(data []byte) Result {
	res := Result{Kind: "pdf"}
	d, err := parsePDF(data)
	if err != nil {
		return Result{Kind: "pdf", Status: StatusError, Note: "The PDF could not be parsed safely (" + err.Error() + ")."}
	}
	if d.encrypted() {
		return Result{Kind: "pdf", Status: StatusProtected, Note: "This PDF is encrypted or password-protected, so its text cannot be read. Open the original."}
	}
	type page struct {
		dict pDict
		res  pDict
	}
	var pages []page
	seen := map[int]bool{}
	var walk func(v any, inherited pDict, depth int)
	walk = func(v any, inherited pDict, depth int) {
		if depth > 32 || len(pages) >= maxPages+1 {
			return
		}
		if r, ok := v.(pRef); ok {
			if seen[r.num] {
				return
			}
			seen[r.num] = true
		}
		n := d.dict(v)
		if n == nil {
			return
		}
		res := inherited
		if r := d.dict(n["Resources"]); r != nil {
			res = r
		}
		if kids := d.array(n["Kids"]); kids != nil && n["Type"] != pName("Page") {
			for _, k := range kids {
				walk(k, res, depth+1)
			}
			return
		}
		pages = append(pages, page{n, res})
	}
	for i := len(d.trailers) - 1; i >= 0 && len(pages) == 0; i-- {
		if root := d.dict(d.trailers[i]["Root"]); root != nil {
			walk(root["Pages"], nil, 0)
		}
	}
	if len(pages) == 0 { // no usable page tree: fall back to page objects in number order
		var nums []int
		for n, v := range d.objs {
			if dd, ok := v.(pDict); ok && dd["Type"] == pName("Page") {
				nums = append(nums, n)
			}
		}
		sort.Ints(nums)
		for _, n := range nums {
			pd := d.objs[n].(pDict)
			pages = append(pages, page{pd, d.dict(pd["Resources"])})
		}
	}
	if len(pages) == 0 {
		return Result{Kind: "pdf", Status: StatusError, Note: "No pages were found in this PDF."}
	}
	res.PageCount = len(pages)
	if len(pages) > maxPages {
		pages = pages[:maxPages]
		res.Truncated = true
	}
	fonts := map[any]*font{}
	work := maxContentTokens
	unsupported := false
	for _, p := range pages {
		var content []byte
		var parts []any
		switch c := p.dict["Contents"].(type) {
		case pArray:
			parts = c
		default:
			if a, ok := d.resolve(c).(pArray); ok {
				parts = a
			} else {
				parts = []any{c}
			}
		}
		for _, part := range parts {
			s, ok := d.resolve(part).(*pStream)
			if !ok {
				continue
			}
			b, err := d.decode(s)
			if err == errBudget {
				res.Truncated = true
				break
			}
			if err == errFilter {
				unsupported = true
			}
			content = append(append(content, b...), '\n')
		}
		e := &extractor{d: d, fonts: fonts, forms: map[int]bool{}, work: &work}
		e.run(content, p.res, identity, 0)
		res.Pages = append(res.Pages, e.out.String())
		if d.budget <= 0 || work <= 0 {
			res.Truncated = true
			break
		}
	}
	if unsupported {
		res.Note = "Some page content uses an encoding this reader does not support; parts of the text may be missing."
	}
	return res
}

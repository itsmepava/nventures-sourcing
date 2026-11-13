package deck

import (
	"bytes"
	"compress/flate"
	"compress/lzw"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
)

// Minimal PDF object model.
type (
	pName   string
	pRef    struct{ num, gen int }
	pDict   map[pName]any
	pArray  []any
	pString []byte
	pKw     string // keyword / operator
	pStream struct {
		dict pDict
		raw  []byte
	}
)

var errDepth = errors.New("nesting too deep")

func isWS(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

func isDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

// lexer reads PDF tokens and values from a byte slice.
type lexer struct {
	b      []byte
	pos    int
	noRefs bool // content streams have no indirect references
}

func (l *lexer) skipWS() {
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		if isWS(c) {
			l.pos++
		} else if c == '%' {
			for l.pos < len(l.b) && l.b[l.pos] != '\n' && l.b[l.pos] != '\r' {
				l.pos++
			}
		} else {
			return
		}
	}
}

func (l *lexer) eof() bool { l.skipWS(); return l.pos >= len(l.b) }

func (l *lexer) regular() []byte {
	s := l.pos
	for l.pos < len(l.b) && !isWS(l.b[l.pos]) && !isDelim(l.b[l.pos]) {
		l.pos++
	}
	return l.b[s:l.pos]
}

func isNumberStart(c byte) bool { return c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9') }

// value parses the next value. It returns io.EOF at end of input and
// (nil, nil) for an unmatched closing delimiter it skips.
func (l *lexer) value(depth int) (any, error) {
	if depth > maxNesting {
		return nil, errDepth
	}
	l.skipWS()
	if l.pos >= len(l.b) {
		return nil, io.EOF
	}
	c := l.b[l.pos]
	switch {
	case c == '/':
		l.pos++
		return pName(decodeName(l.regular())), nil
	case c == '(':
		return l.literal(), nil
	case c == '<':
		if l.pos+1 < len(l.b) && l.b[l.pos+1] == '<' {
			l.pos += 2
			d := pDict{}
			for {
				l.skipWS()
				if l.pos >= len(l.b) {
					return d, nil
				}
				if l.b[l.pos] == '>' {
					l.pos++
					if l.pos < len(l.b) && l.b[l.pos] == '>' {
						l.pos++
					}
					return d, nil
				}
				k, err := l.value(depth + 1)
				if err != nil {
					return d, err
				}
				key, ok := k.(pName)
				if !ok {
					continue // tolerate junk keys
				}
				v, err := l.value(depth + 1)
				if err != nil {
					return d, err
				}
				if kw, ok := v.(pKw); ok && (kw == ">>" || kw == "endobj") {
					return d, nil
				}
				d[key] = v
			}
		}
		return l.hexString(), nil
	case c == '[':
		l.pos++
		var a pArray
		for {
			l.skipWS()
			if l.pos >= len(l.b) {
				return a, nil
			}
			if l.b[l.pos] == ']' {
				l.pos++
				return a, nil
			}
			v, err := l.value(depth + 1)
			if err != nil {
				return a, err
			}
			if kw, ok := v.(pKw); ok && kw == "endobj" {
				return a, nil
			}
			a = append(a, v)
		}
	case c == ']' || c == '>' || c == ')' || c == '{' || c == '}':
		l.pos++
		if c == '>' && l.pos < len(l.b) && l.b[l.pos] == '>' {
			l.pos++
			return pKw(">>"), nil
		}
		return nil, nil
	case isNumberStart(c):
		tok := l.regular()
		if len(tok) == 0 {
			l.pos++
			return nil, nil
		}
		f, err := strconv.ParseFloat(string(tok), 64)
		if err != nil {
			return pKw(tok), nil
		}
		if !l.noRefs && isInt(tok) {
			save := l.pos
			l.skipWS()
			g := l.regular()
			if isInt(g) {
				l.skipWS()
				if l.pos < len(l.b) && l.b[l.pos] == 'R' && (l.pos+1 == len(l.b) || isWS(l.b[l.pos+1]) || isDelim(l.b[l.pos+1])) {
					l.pos++
					gen, _ := strconv.Atoi(string(g))
					return pRef{int(f), gen}, nil
				}
			}
			l.pos = save
		}
		return f, nil
	}
	tok := l.regular()
	if len(tok) == 0 {
		l.pos++
		return nil, nil
	}
	switch string(tok) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	return pKw(tok), nil
}

// clampInt converts a PDF number to an int in [lo, hi]. PDF numbers are
// untrusted floats: a plain int() of a huge value overflows.
func clampInt(f float64, lo, hi int) int {
	switch {
	case f != f || f <= float64(lo):
		return lo
	case f >= float64(hi):
		return hi
	}
	return int(f)
}

func isInt(b []byte) bool {
	if len(b) == 0 || len(b) > 10 {
		return false
	}
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func decodeName(b []byte) string {
	if bytes.IndexByte(b, '#') < 0 {
		return string(b)
	}
	var out []byte
	for i := 0; i < len(b); i++ {
		if b[i] == '#' && i+2 < len(b) {
			if v, err := strconv.ParseUint(string(b[i+1:i+3]), 16, 8); err == nil {
				out = append(out, byte(v))
				i += 2
				continue
			}
		}
		out = append(out, b[i])
	}
	return string(out)
}

func (l *lexer) literal() pString {
	l.pos++ // (
	var out []byte
	depth := 1
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		switch c {
		case '(':
			depth++
			out = append(out, c)
		case ')':
			depth--
			if depth == 0 {
				return out
			}
			out = append(out, c)
		case '\\':
			if l.pos >= len(l.b) {
				return out
			}
			e := l.b[l.pos]
			l.pos++
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r':
				if l.pos < len(l.b) && l.b[l.pos] == '\n' {
					l.pos++
				}
			case '\n':
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for k := 0; k < 2 && l.pos < len(l.b) && l.b[l.pos] >= '0' && l.b[l.pos] <= '7'; k++ {
						v = v*8 + int(l.b[l.pos]-'0')
						l.pos++
					}
					out = append(out, byte(v))
				} else {
					out = append(out, e)
				}
			}
		default:
			out = append(out, c)
		}
	}
	return out
}

func (l *lexer) hexString() pString {
	l.pos++ // <
	var digits []byte
	for l.pos < len(l.b) && l.b[l.pos] != '>' {
		c := l.b[l.pos]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			digits = append(digits, c)
		}
		l.pos++
	}
	if l.pos < len(l.b) {
		l.pos++ // >
	}
	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}
	out := make([]byte, len(digits)/2)
	hex.Decode(out, digits)
	return out
}

// ---- document ----

type pdfDoc struct {
	b        []byte
	objs     map[int]any
	trailers []pDict
	budget   int64 // remaining decompressed bytes
}

var objRe = regexp.MustCompile(`(\d{1,10})[ \t\r\n\f\x00]+(\d{1,5})[ \t\r\n\f\x00]+obj\b`)

func parsePDF(b []byte) (*pdfDoc, error) {
	d := &pdfDoc{b: b, objs: map[int]any{}, budget: maxDecodedTotal}
	pos := 0
	for pos < len(b) {
		loc := objRe.FindSubmatchIndex(b[pos:])
		if loc == nil {
			break
		}
		num, _ := strconv.Atoi(string(b[pos+loc[2] : pos+loc[3]]))
		l := &lexer{b: b, pos: pos + loc[1]}
		v, err := l.value(0)
		if err == errDepth {
			return nil, err
		}
		l.skipWS()
		l.pos = min(l.pos, len(b))
		if dict, ok := v.(pDict); ok && bytes.HasPrefix(b[l.pos:], []byte("stream")) {
			start := l.pos + len("stream")
			if start < len(b) && b[start] == '\r' {
				start++
			}
			if start < len(b) && b[start] == '\n' {
				start++
			}
			end := -1
			if n, ok := dict["Length"].(float64); ok && n >= 0 && n <= float64(len(b)-start) {
				e := start + clampInt(n, 0, len(b)-start)
				k := e
				for k < len(b) && isWS(b[k]) {
					k++
				}
				if bytes.HasPrefix(b[k:], []byte("endstream")) {
					end = e
					l.pos = k + len("endstream")
				}
			}
			if end < 0 {
				i := bytes.Index(b[start:], []byte("endstream"))
				if i < 0 {
					end, l.pos = len(b), len(b)
				} else {
					end = start + i
					l.pos = end + len("endstream")
					for end > start && (b[end-1] == '\n' || b[end-1] == '\r') {
						end--
					}
				}
			}
			v = &pStream{dict: dict, raw: b[start:end]}
		}
		d.objs[num] = v
		if len(d.objs) > maxObjects {
			return nil, errors.New("too many objects")
		}
		if l.pos <= pos+loc[1] {
			l.pos = pos + loc[1]
		}
		pos = l.pos
	}
	// Classic trailers.
	for i := 0; ; {
		j := bytes.Index(b[i:], []byte("trailer"))
		if j < 0 {
			break
		}
		l := &lexer{b: b, pos: i + j + len("trailer")}
		if v, err := l.value(0); err == nil {
			if t, ok := v.(pDict); ok {
				d.trailers = append(d.trailers, t)
			}
		}
		i += j + len("trailer")
	}
	// Cross-reference streams double as trailers; object streams hold objects.
	nums := make([]int, 0, len(d.objs))
	for n := range d.objs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		s, ok := d.objs[n].(*pStream)
		if !ok {
			continue
		}
		switch s.dict["Type"] {
		case pName("XRef"):
			d.trailers = append(d.trailers, s.dict)
		case pName("ObjStm"):
			d.expandObjStm(s)
		}
	}
	return d, nil
}

func (d *pdfDoc) expandObjStm(s *pStream) {
	data, err := d.decode(s)
	if err != nil {
		return
	}
	firstF, _ := s.dict["First"].(float64)
	countF, _ := s.dict["N"].(float64)
	first, count := clampInt(firstF, 0, len(data)+1), clampInt(countF, 0, maxObjects+1)
	if first <= 0 || first > len(data) || count <= 0 || count > maxObjects {
		return
	}
	hdr := &lexer{b: data[:first], noRefs: true}
	type entry struct{ num, off int }
	var entries []entry
	for i := 0; i < count; i++ {
		a, e1 := hdr.value(0)
		b, e2 := hdr.value(0)
		num, ok1 := a.(float64)
		off, ok2 := b.(float64)
		if e1 != nil || e2 != nil || !ok1 || !ok2 {
			break
		}
		p := first + clampInt(off, -1, len(data))
		if p < first || p >= len(data) || num < 0 || num > 1<<31 {
			continue
		}
		entries = append(entries, entry{int(num), p})
	}
	// Each object is parsed only up to the next object's offset, so
	// overlapping or repeated offsets cannot cause repeated full parses.
	sort.Slice(entries, func(i, j int) bool { return entries[i].off < entries[j].off })
	for i, en := range entries {
		end := len(data)
		if i+1 < len(entries) {
			end = entries[i+1].off
		}
		if end <= en.off {
			continue
		}
		if _, exists := d.objs[en.num]; exists {
			continue
		}
		l := &lexer{b: data[:end], pos: en.off}
		v, err := l.value(0)
		if err != nil {
			continue
		}
		d.objs[en.num] = v
		if len(d.objs) > maxObjects {
			return
		}
	}
}

func (d *pdfDoc) resolve(v any) any {
	for i := 0; i < 32; i++ {
		r, ok := v.(pRef)
		if !ok {
			return v
		}
		v = d.objs[r.num]
	}
	return nil
}

func (d *pdfDoc) dict(v any) pDict {
	switch t := d.resolve(v).(type) {
	case pDict:
		return t
	case *pStream:
		return t.dict
	}
	return nil
}

func (d *pdfDoc) array(v any) pArray {
	a, _ := d.resolve(v).(pArray)
	return a
}

func (d *pdfDoc) num(v any, def float64) float64 {
	if f, ok := d.resolve(v).(float64); ok {
		return f
	}
	return def
}

func (d *pdfDoc) encrypted() bool {
	for _, t := range d.trailers {
		if _, ok := t["Encrypt"]; ok {
			return true
		}
	}
	return false
}

var errFilter = errors.New("unsupported filter")

// decode applies the stream's filters within the decompression budget.
func (d *pdfDoc) decode(s *pStream) ([]byte, error) {
	var filters []pName
	switch f := d.resolve(s.dict["Filter"]).(type) {
	case pName:
		filters = []pName{f}
	case pArray:
		for _, x := range f {
			if n, ok := d.resolve(x).(pName); ok {
				filters = append(filters, n)
			}
		}
	}
	var parms []pDict
	switch p := d.resolve(s.dict["DecodeParms"]).(type) {
	case pDict:
		parms = []pDict{p}
	case pArray:
		for _, x := range p {
			parms = append(parms, d.dict(x))
		}
	}
	data := s.raw
	for i, f := range filters {
		var parm pDict
		if i < len(parms) {
			parm = parms[i]
		}
		limit := int64(maxDecodedPerStream)
		if d.budget < limit {
			limit = d.budget
		}
		if limit <= 0 {
			return nil, errBudget
		}
		var r io.Reader
		switch f {
		case "FlateDecode", "Fl":
			if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
				r = zr
			} else {
				r = flate.NewReader(bytes.NewReader(data))
			}
		case "LZWDecode", "LZW":
			if ec := d.num(parm["EarlyChange"], 1); ec != 1 {
				return nil, errFilter
			}
			r = lzw.NewReader(bytes.NewReader(data), lzw.MSB, 8)
		case "ASCIIHexDecode", "AHx":
			lx := &lexer{b: append(append([]byte{'<'}, data...), '>')}
			data = lx.hexString()
			continue
		case "ASCII85Decode", "A85":
			src := bytes.TrimSpace(data)
			src = bytes.TrimPrefix(src, []byte("<~"))
			if i := bytes.Index(src, []byte("~>")); i >= 0 {
				src = src[:i]
			}
			r = ascii85.NewDecoder(bytes.NewReader(src))
		case "RunLengthDecode", "RL":
			out, err := runLength(data, limit)
			if err != nil {
				return nil, err
			}
			d.budget -= int64(len(out))
			data = out
			continue
		default:
			return nil, errFilter // images (DCT/JPX/CCITT/JBIG2) and unknown filters
		}
		out, err := io.ReadAll(io.LimitReader(r, limit+1))
		if int64(len(out)) > limit {
			return nil, errBudget
		}
		if err != nil && len(out) == 0 {
			return nil, err
		}
		d.budget -= int64(len(out))
		if p := clampInt(d.num(parm["Predictor"], 1), 0, 100); p >= 10 {
			out = pngUnpredict(out, clampInt(d.num(parm["Columns"], 1), 1, 1<<20), clampInt(d.num(parm["Colors"], 1), 1, 64), clampInt(d.num(parm["BitsPerComponent"], 8), 1, 32))
		}
		data = out
	}
	return data, nil
}

func runLength(src []byte, limit int64) ([]byte, error) {
	var out []byte
	for i := 0; i < len(src); {
		n := int(src[i])
		i++
		switch {
		case n == 128:
			return out, nil
		case n < 128:
			if i+n+1 > len(src) {
				return append(out, src[i:]...), nil
			}
			out = append(out, src[i:i+n+1]...)
			i += n + 1
		default:
			if i >= len(src) {
				return out, nil
			}
			out = append(out, bytes.Repeat(src[i:i+1], 257-n)...)
			i++
		}
		if int64(len(out)) > limit {
			return nil, errBudget
		}
	}
	return out, nil
}

func pngUnpredict(data []byte, columns, colors, bpc int) []byte {
	bpp := (colors*bpc + 7) / 8
	if bpp < 1 {
		bpp = 1
	}
	rowLen := (columns*colors*bpc + 7) / 8
	if rowLen < 1 || columns > 1<<20 {
		return data
	}
	var out []byte
	prev := make([]byte, rowLen)
	for i := 0; i+1+rowLen <= len(data); i += 1 + rowLen {
		ft := data[i]
		row := append([]byte(nil), data[i+1:i+1+rowLen]...)
		for j := range row {
			var a, b, c byte
			if j >= bpp {
				a, c = row[j-bpp], prev[j-bpp]
			}
			b = prev[j]
			switch ft {
			case 1:
				row[j] += a
			case 2:
				row[j] += b
			case 3:
				row[j] += byte((int(a) + int(b)) / 2)
			case 4:
				p := int(a) + int(b) - int(c)
				pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
				switch {
				case pa <= pb && pa <= pc:
					row[j] += a
				case pb <= pc:
					row[j] += b
				default:
					row[j] += c
				}
			}
		}
		out = append(out, row...)
		prev = row
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

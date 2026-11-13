package deck

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

var errBudget = errors.New("decompression budget exceeded")

type zipReader struct {
	files  map[string]*zip.File
	budget int64
}

func (z *zipReader) read(name string) ([]byte, error) {
	f := z.files[name]
	if f == nil {
		return nil, errors.New("missing part")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	limit := int64(maxDecodedPerStream)
	if z.budget < limit {
		limit = z.budget
	}
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errBudget
	}
	z.budget -= int64(len(b))
	return b, nil
}

type rel struct{ ID, Type, Target string }

// rels reads an OOXML relationships part and resolves targets against dir.
func (z *zipReader) rels(relsPath, dir string) map[string]rel {
	out := map[string]rel{}
	b, err := z.read(relsPath)
	if err != nil {
		return out
	}
	var doc struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return out
	}
	for _, r := range doc.Rels {
		if strings.EqualFold(r.Mode, "External") {
			continue
		}
		t := r.Target
		if strings.HasPrefix(t, "/") {
			t = strings.TrimPrefix(t, "/")
		} else {
			t = path.Clean(path.Join(dir, t))
		}
		out[r.ID] = rel{r.ID, r.Type, t}
	}
	return out
}

func readPPTX(data []byte) Result {
	res := Result{Kind: "pptx"}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Result{Kind: "zip", Status: StatusError, Note: "The file is not a readable PPTX archive."}
	}
	if len(zr.File) > maxZipEntries {
		return Result{Kind: "pptx", Status: StatusError, Note: "The archive has too many parts to read safely."}
	}
	z := &zipReader{files: map[string]*zip.File{}, budget: maxDecodedTotal}
	for _, f := range zr.File {
		z.files[path.Clean(strings.TrimPrefix(f.Name, "/"))] = f
	}
	if z.files["ppt/presentation.xml"] == nil {
		return Result{Kind: "zip", Status: StatusUnsupported, Note: "This archive is not a PowerPoint deck. Only PDF and PPTX decks are read."}
	}

	// Slide order comes from presentation.xml's slide list, not file names.
	var slides []string
	if b, err := z.read("ppt/presentation.xml"); err == nil {
		prels := z.rels("ppt/_rels/presentation.xml.rels", "ppt")
		dec := xml.NewDecoder(bytes.NewReader(b))
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sldId" {
				for _, a := range se.Attr {
					if a.Name.Local == "id" && a.Name.Space != "" {
						if r, ok := prels[a.Value]; ok && z.files[r.Target] != nil {
							slides = append(slides, r.Target)
						}
					}
				}
			}
		}
	}
	if len(slides) == 0 { // fall back to numeric file order
		for name := range z.files {
			if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
				slides = append(slides, name)
			}
		}
		num := func(s string) int {
			n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(s, "ppt/slides/slide"), ".xml"))
			return n
		}
		sort.Slice(slides, func(i, j int) bool { return num(slides[i]) < num(slides[j]) })
	}
	res.PageCount = len(slides)
	if len(slides) > maxPages {
		slides = slides[:maxPages]
		res.Truncated = true
	}
	for _, s := range slides {
		b, err := z.read(s)
		if err == errBudget {
			res.Truncated = true
			break
		}
		if err != nil {
			res.Pages = append(res.Pages, "")
			continue
		}
		text := drawingText(b)
		// SmartArt text lives in separate diagram parts referenced by the slide.
		dir := path.Dir(s)
		for _, r := range z.rels(path.Join(dir, "_rels", path.Base(s)+".rels"), dir) {
			if strings.HasSuffix(r.Type, "/diagramData") {
				if d, err := z.read(r.Target); err == nil {
					if t := drawingText(d); t != "" {
						text += "\n" + t
					}
				}
			}
		}
		res.Pages = append(res.Pages, text)
	}
	return res
}

// drawingText collects DrawingML text: a:t runs, with a:p paragraphs and
// a:br breaks as newlines and a:tab as a space. Table cells are joined on
// one line per row.
func drawingText(b []byte) string {
	var sb strings.Builder
	dec := xml.NewDecoder(bytes.NewReader(b))
	inT, inCell := 0, 0
	for sb.Len() <= 4*MaxTextRunes {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inT++
			case "br":
				sb.WriteByte('\n')
			case "tab":
				sb.WriteByte(' ')
			case "tc":
				inCell++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				if inT > 0 {
					inT--
				}
			case "p":
				if inCell > 0 {
					sb.WriteByte(' ')
				} else {
					sb.WriteByte('\n')
				}
			case "tc":
				if inCell > 0 {
					inCell--
				}
				sb.WriteString("  ")
			case "tr":
				sb.WriteByte('\n')
			}
		case xml.CharData:
			if inT > 0 {
				sb.Write(t)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

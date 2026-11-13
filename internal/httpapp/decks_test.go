package httpapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/itsmepava/nventures-sourcing/internal/config"
	"github.com/itsmepava/nventures-sourcing/internal/deck"
	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

func pitchByName(t *testing.T, a *App, name string) store.Pitch {
	t.Helper()
	ps, _, err := a.Store.Pitches(t.Context(), store.ListQuery{Q: name, PerPage: 5})
	if err != nil || len(ps) == 0 {
		t.Fatalf("pitch %q not found: %v", name, err)
	}
	return ps[0]
}

func TestReadDeckAttachments(t *testing.T) {
	a, srv := newTestApp(t, nil)
	admin := adminClient(t, srv)
	do(t, admin, "POST", srv.URL+"/api/inbox/import", jsonHdr(), `{"limit":50}`)
	a.Jobs.Wait()
	responder := addMember(t, srv, admin, "Rae Responder", "responder@nventures-demo.test", "responder")
	viewer := addMember(t, srv, admin, "Vic Viewer", "viewer@nventures-demo.test", "viewer")

	cases := []struct {
		pitch, file, kind string
		pages             int
		facts             map[string]string
	}{
		{"Tidewell", "Tidewell-deck-demo.pdf", "pdf", 5, map[string]string{"hq": "Chattogram, Bangladesh", "founders": "Ravi Menon and Tahmina Akter", "raising": "Now raising a USD 600,000 pre-seed round", "website": "https://tidewell.nventures-demo.test"}},
		{"Byte Bazaar", "ByteBazaar-deck-demo.pptx", "pptx", 5, map[string]string{"hq": "Lahore, Pakistan", "description": "Byte Bazaar is a B2B marketplace for refurbished laptops for private schools in Pakistan", "contact": "hello@bytebazaar.nventures-demo.test"}},
	}
	for _, c := range cases {
		before := pitchByName(t, a, c.pitch)
		var d extract.Pitch
		json.Unmarshal(before.Data, &d)
		if len(d.Attachments) != 1 || d.Attachments[0].PartID == "" {
			t.Fatalf("%s: attachment indicator/part id missing: %+v", c.pitch, d.Attachments)
		}
		path := fmt.Sprintf("%s/api/pitches/%d/decks", srv.URL, before.ID)
		body := fmt.Sprintf(`{"part_id":%q,"filename":%q}`, d.Attachments[0].PartID, c.file)
		if r := do(t, viewer, "POST", path, jsonHdr(), body); r.StatusCode != 403 {
			t.Fatalf("viewer read a deck: %d", r.StatusCode)
		}
		r := do(t, responder, "POST", path, jsonHdr(), body)
		var got store.PitchDeck
		json.NewDecoder(r.Body).Decode(&got)
		if r.StatusCode != 200 || got.Status != deck.StatusExtracted || got.Kind != c.kind || got.PageCount != c.pages || got.SHA256 == "" || got.ReadByName != "Rae Responder" {
			t.Fatalf("%s: %d %+v", c.pitch, r.StatusCode, got)
		}
		var facts []deck.Fact
		json.Unmarshal(got.Facts, &facts)
		found := map[string]string{}
		for _, f := range facts {
			if _, ok := found[f.Field]; !ok {
				found[f.Field] = f.Value
			}
		}
		for k, v := range c.facts {
			if found[k] != v {
				t.Errorf("%s: fact %s = %q, want %q (%v)", c.pitch, k, found[k], v, found)
			}
		}
		// Reading a deck never changes the pitch: fields, status and notes stay as reviewed.
		after := pitchByName(t, a, c.pitch)
		if after.Status != before.Status || string(after.Data) != string(before.Data) || after.UpdatedAt != before.UpdatedAt {
			t.Errorf("%s: pitch changed by reading its deck", c.pitch)
		}
		// Re-reading replaces the stored reading rather than adding a second one.
		do(t, admin, "POST", path, jsonHdr(), body)
		detail := decode(t, do(t, viewer, "GET", fmt.Sprintf("%s/api/pitches/%d", srv.URL, before.ID), nil, ""))
		if decks, _ := detail["decks"].([]any); len(decks) != 1 {
			t.Errorf("%s: expected one stored reading, got %d", c.pitch, len(decks))
		}
	}

	// Deck text is data: the Tidewell deck asks automated reviewers to approve
	// it, and the pitch stays in review.
	tide := pitchByName(t, a, "Tidewell")
	decks, _ := a.Store.Decks(t.Context(), tide.ID)
	if !strings.Contains(string(decks[0].Pages), "ignore previous instructions") || tide.Status != "review" {
		t.Errorf("injection fixture: status %s", tide.Status)
	}
	ev, _ := a.Store.Events(t.Context(), 0, "deck_read", 0, 10)
	if len(ev) < 2 || strings.Contains(ev[0].Message, "Chattogram") {
		t.Errorf("audit: %d events; deck text must not be logged: %+v", len(ev), ev)
	}

	// Errors: unknown attachment, unsupported type, bad body.
	path := fmt.Sprintf("%s/api/pitches/%d/decks", srv.URL, tide.ID)
	if r := do(t, admin, "POST", path, jsonHdr(), `{"filename":"other.pdf"}`); r.StatusCode != 404 {
		t.Errorf("unknown attachment: %d", r.StatusCode)
	}
	if r := do(t, admin, "POST", path, jsonHdr(), `{}`); r.StatusCode != 400 {
		t.Errorf("empty body: %d", r.StatusCode)
	}
	var d extract.Pitch
	json.Unmarshal(tide.Data, &d)
	d.Attachments = append(d.Attachments, extract.Attachment{PartID: "9", Filename: "photo.png", MimeType: "image/png"})
	tide.Data, _ = json.Marshal(d)
	a.Store.UpdatePitch(t.Context(), tide)
	if r := do(t, admin, "POST", path, jsonHdr(), `{"part_id":"9","filename":"photo.png"}`); r.StatusCode != 400 {
		t.Errorf("image attachment: %d", r.StatusCode)
	}
}

func TestReadDeckNeedsGoogleInLiveMode(t *testing.T) {
	a, srv := newTestApp(t, func(c *config.Config) { c.Mode = "live" })
	admin := adminClient(t, srv)
	data, _ := json.Marshal(extract.Pitch{Name: "Live Co", MessageID: "abc123", Attachments: []extract.Attachment{{PartID: "2", Filename: "deck.pdf", MimeType: "application/pdf", Size: 1000}}})
	p, err := a.Store.InsertPitch(t.Context(), store.Pitch{MessageID: "me@example.test:abc123", Account: "me@example.test", Mode: "live", Name: "Live Co", Status: "review", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	r := do(t, admin, "POST", fmt.Sprintf("%s/api/pitches/%d/decks", srv.URL, p.ID), jsonHdr(), `{"part_id":"2","filename":"deck.pdf"}`)
	m := decode(t, r)
	if r.StatusCode != 409 || !strings.Contains(fmt.Sprint(m["error"]), "Google is not connected") {
		t.Fatalf("live without Google: %d %v", r.StatusCode, m)
	}
	// Oversized attachments are recorded as too large without downloading.
	data, _ = json.Marshal(extract.Pitch{Name: "Big Co", MessageID: "big1", Attachments: []extract.Attachment{{PartID: "2", Filename: "huge.pdf", MimeType: "application/pdf", Size: deck.MaxInputBytes + 1}}})
	big, _ := a.Store.InsertPitch(t.Context(), store.Pitch{MessageID: "me@example.test:big1", Account: "me@example.test", Mode: "live", Name: "Big Co", Status: "review", Data: data})
	var got store.PitchDeck
	r = do(t, admin, "POST", fmt.Sprintf("%s/api/pitches/%d/decks", srv.URL, big.ID), jsonHdr(), `{"part_id":"2","filename":"huge.pdf"}`)
	json.NewDecoder(r.Body).Decode(&got)
	if r.StatusCode != 200 || got.Status != deck.StatusTooLarge {
		t.Fatalf("too large: %d %+v", r.StatusCode, got)
	}
}

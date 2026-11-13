package httpapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/deck"
	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

// findPart locates an attachment part in a Gmail message, by part ID when
// known, otherwise by file name.
func findPart(p extract.GmailPart, partID, filename string) *extract.GmailPart {
	if p.Filename != "" && ((partID != "" && p.PartID == partID) || (partID == "" && p.Filename == filename)) {
		return &p
	}
	for _, c := range p.Parts {
		if f := findPart(c, partID, filename); f != nil {
			return f
		}
	}
	return nil
}

// readDeck reads one PDF/PPTX attachment of a pitch on request: it fetches
// the message and attachment read-only from Gmail, extracts text with
// internal/deck and stores the text and reviewable facts. Nothing about the
// pitch itself changes; the reviewer decides what to use.
func (a *App) readDeck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathID(r)
	p, err := a.Store.Pitch(ctx, id)
	if !ok || err != nil {
		writeErr(w, 404, "Pitch not found")
		return
	}
	var in struct {
		PartID   string `json:"part_id"`
		Filename string `json:"filename"`
	}
	if err := readJSON(r, &in); err != nil || in.Filename == "" {
		writeErr(w, 400, "Choose an attachment to read")
		return
	}
	var d extract.Pitch
	_ = json.Unmarshal(p.Data, &d)
	var att *extract.Attachment
	for i := range d.Attachments {
		x := &d.Attachments[i]
		if x.Filename == in.Filename && (in.PartID == "" || x.PartID == in.PartID) {
			att = x
			break
		}
	}
	if att == nil {
		writeErr(w, 404, "That attachment is not part of this pitch")
		return
	}
	if !deck.Supported(att.Filename, att.MimeType) {
		writeErr(w, 400, "Only PDF and PowerPoint (.pptx) decks can be read. Open the original email for other files.")
		return
	}
	row := store.PitchDeck{PitchID: p.ID, PartID: att.PartID, Filename: att.Filename, MimeType: att.MimeType, Size: int64(att.Size)}
	if u, ok := currentUser(r); ok {
		row.ReadBy = &u.ID
	}
	data, status, msg := a.fetchAttachment(ctx, p, d, att)
	switch {
	case status == http.StatusRequestEntityTooLarge:
		row.Status, row.Kind, row.Note = deck.StatusTooLarge, "", "Files over 20 MB are not read. Open the original email instead."
	case status != 0:
		writeErr(w, status, msg)
		return
	default:
		sum := sha256.Sum256(data)
		res := deck.Extract(att.Filename, att.MimeType, data)
		row.Size, row.SHA256 = int64(len(data)), hex.EncodeToString(sum[:])
		row.Kind, row.Status, row.Note, row.PageCount, row.Truncated = res.Kind, res.Status, res.Note, res.PageCount, res.Truncated
		row.Pages, _ = json.Marshal(res.Pages)
		row.Facts, _ = json.Marshal(deck.Facts(res.Pages, res.Kind == "pdf"))
	}
	saved, err := a.Store.SaveDeck(ctx, row)
	if err != nil {
		writeErr(w, 500, "Could not save the deck reading")
		return
	}
	a.Store.Audit(ctx, 0, "deck_read", fmt.Sprintf("Pitch #%d: read attachment %q (%s, %d pages)", p.ID, att.Filename, saved.Status, saved.PageCount), map[string]any{"pitch_id": p.ID, "deck_id": saved.ID, "status": saved.Status})
	decks, _ := a.Store.Decks(ctx, p.ID)
	for _, x := range decks {
		if x.ID == saved.ID {
			saved = x
		}
	}
	writeJSON(w, 200, saved)
}

// fetchAttachment downloads the attachment bytes. A non-zero status is an
// HTTP error to return (413 means "too large; record it without reading").
func (a *App) fetchAttachment(ctx context.Context, p store.Pitch, d extract.Pitch, att *extract.Attachment) ([]byte, int, string) {
	if att.Size > deck.MaxInputBytes {
		return nil, http.StatusRequestEntityTooLarge, ""
	}
	msgID := d.MessageID
	if msgID == "" {
		if i := strings.LastIndexByte(p.MessageID, ':'); i >= 0 {
			msgID = p.MessageID[i+1:]
		}
	}
	if msgID == "" || p.LegacyRef != "" && d.MessageID == "" {
		return nil, 409, "This pitch has no Gmail message to read the attachment from (it was imported from the previous app). Open the original email instead."
	}
	conn, err := a.Connect(ctx)
	if err != nil {
		return nil, 409, err.Error()
	}
	if conn.Account != "" && p.Account != "" && !strings.EqualFold(conn.Account, p.Account) {
		return nil, 409, "This pitch came from a different mailbox than the one connected now. Reconnect that mailbox to read its attachments."
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	msg, err := conn.Gmail.Get(ctx, msgID)
	if err != nil {
		return nil, 502, providerMsg(err, "Could not load the email from Gmail")
	}
	part := findPart(msg.Payload, att.PartID, att.Filename)
	if part == nil && att.PartID != "" {
		part = findPart(msg.Payload, "", att.Filename)
	}
	if part == nil {
		return nil, 404, "The attachment is no longer in this email."
	}
	if part.Body.Size > deck.MaxInputBytes {
		return nil, http.StatusRequestEntityTooLarge, ""
	}
	if part.Body.AttachmentID == "" {
		if part.Body.Data == "" {
			return nil, 404, "The attachment has no content."
		}
		return decodeInline(part.Body.Data), 0, ""
	}
	data, err := conn.Gmail.Attachment(ctx, msgID, part.Body.AttachmentID)
	if err != nil {
		return nil, 502, providerMsg(err, "Could not download the attachment from Gmail")
	}
	if len(data) > deck.MaxInputBytes {
		return nil, http.StatusRequestEntityTooLarge, ""
	}
	return data, 0, ""
}

func providerMsg(err error, fallback string) string {
	var pe *netx.ProviderError
	if errors.As(err, &pe) {
		return fallback + ": " + pe.Msg
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fallback + ": timed out"
	}
	return fallback
}

func decodeInline(s string) []byte {
	var p extract.GmailPart
	p.Body.Data = s
	return extract.PartBytes(p)
}

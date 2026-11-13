package extract

import (
	"encoding/base64"
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/itsmepava/nventures-sourcing/internal/domain"
)

// GmailHeader is a Gmail API message header.
type GmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GmailPart mirrors the Gmail API MessagePart (format=full).
type GmailPart struct {
	PartID   string        `json:"partId,omitempty"`
	MimeType string        `json:"mimeType"`
	Filename string        `json:"filename,omitempty"`
	Headers  []GmailHeader `json:"headers,omitempty"`
	Body     struct {
		AttachmentID string `json:"attachmentId,omitempty"`
		Size         int    `json:"size"`
		Data         string `json:"data,omitempty"`
	} `json:"body"`
	Parts []GmailPart `json:"parts,omitempty"`
}

// GmailMessage mirrors the Gmail API Message resource.
type GmailMessage struct {
	ID           string    `json:"id"`
	ThreadID     string    `json:"threadId"`
	LabelIDs     []string  `json:"labelIds,omitempty"`
	Snippet      string    `json:"snippet,omitempty"`
	InternalDate string    `json:"internalDate,omitempty"`
	Payload      GmailPart `json:"payload"`
}

// Header returns the first header with the given name.
func (m GmailMessage) Header(name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// Attachment is an attachment indicator; contents are never parsed silently.
type Attachment struct {
	PartID   string `json:"part_id,omitempty"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
	Reviewed bool   `json:"reviewed"`
}

func decodeB64(s string) []byte {
	s = strings.TrimRight(s, "=")
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		b, _ = base64.RawStdEncoding.DecodeString(s)
	}
	return b
}

// PartBytes returns a part's inline body bytes (base64url-decoded).
func PartBytes(p GmailPart) []byte { return decodeB64(p.Body.Data) }

func charsetOf(p GmailPart) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			if i := strings.Index(strings.ToLower(h.Value), "charset="); i >= 0 {
				return strings.Trim(strings.ToLower(strings.Fields(h.Value[i+8:] + " ")[0]), `"';`)
			}
		}
	}
	return ""
}

func toUTF8(b []byte, charset string) string {
	if utf8.Valid(b) {
		return string(b)
	}
	switch charset {
	case "iso-8859-1", "latin1", "windows-1252", "us-ascii", "":
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r)
	}
	return strings.ToValidUTF8(string(b), "�")
}

// Bodies returns plain and HTML bodies and attachment indicators.
func Bodies(p GmailPart) (plain, htmlBody string, atts []Attachment) {
	var walk func(p GmailPart)
	walk = func(p GmailPart) {
		mt := strings.ToLower(p.MimeType)
		if p.Filename != "" {
			atts = append(atts, Attachment{PartID: p.PartID, Filename: p.Filename, MimeType: p.MimeType, Size: p.Body.Size})
			return
		}
		if p.Body.Data != "" {
			s := toUTF8(decodeB64(p.Body.Data), charsetOf(p))
			switch mt {
			case "text/plain":
				plain += s + "\n"
			case "text/html":
				htmlBody += s + "\n"
			}
		}
		for _, c := range p.Parts {
			walk(c)
		}
	}
	walk(p)
	return
}

// BodyText returns the best text representation: text/plain, else HTML converted.
func BodyText(p GmailPart) string {
	plain, h, _ := Bodies(p)
	if strings.TrimSpace(plain) != "" {
		return plain
	}
	return HTMLToText(h)
}

type label struct {
	key    string
	names  []string
	strict bool // require a colon (generic words)
}

var labels = []label{
	{"company", []string{"Company name", "Startup name", "Name of company", "Name of the company", "Company / startup name"}, false},
	{"company", []string{"Company", "Startup"}, true},
	{"founder", []string{"Full name", "Founder name", "Founder", "Founders", "Your name"}, false},
	{"website", []string{"Website", "Company website", "Website URL", "Web"}, false},
	{"stage", []string{"Stage", "Current stage", "Funding stage"}, false},
	{"sector", []string{"Sector", "Industry", "Vertical"}, false},
	{"incorporation", []string{"Country of incorporation", "Incorporated in", "Place of incorporation"}, false},
	{"incorporation_year", []string{"Year of incorporation", "Incorporation year", "Year incorporated", "Founded"}, false},
	{"hq", []string{"Headquarters", "HQ", "Headquartered in", "Based in"}, false},
	{"location", []string{"Location", "City", "Country"}, true},
	{"description", []string{"One line on what you do", "What do you do", "What does your company do", "Company description", "Description", "Elevator pitch", "About the company", "About"}, false},
	{"deck", []string{"Link to deck (DocSend / Drive)", "Pitch deck", "Deck link", "Deck"}, false},
	{"contact", []string{"Contact email", "Founder email", "Email address", "Email"}, false},
	{"phone", []string{"Phone", "Phone number", "Mobile"}, true},
	{"raising", []string{"Amount raising", "Raising", "Funding ask", "Ask", "Round size"}, true},
	{"funding", []string{"Funding raised", "Total raised", "Raised to date"}, true},
	{"revenue", []string{"Revenue", "MRR", "ARR"}, true},
	{"source", []string{"How did you hear about us?", "How did you hear about us", "Referral"}, false},
	{"linkedin", []string{"LinkedIn", "Founder LinkedIn", "Company LinkedIn"}, true},
}

var labelRe, labelKeys = buildLabelRe()

func buildLabelRe() (*regexp.Regexp, map[string]label) {
	keys := map[string]label{}
	var loose, strict []string
	for _, l := range labels {
		for _, n := range l.names {
			keys[strings.ToLower(n)] = l
			if l.strict {
				strict = append(strict, regexp.QuoteMeta(n))
			} else {
				loose = append(loose, regexp.QuoteMeta(n))
			}
		}
	}
	// Longest first so "Company name" wins over "Company".
	byLen := func(s []string) { sort.Slice(s, func(i, j int) bool { return len(s[i]) > len(s[j]) }) }
	byLen(loose)
	byLen(strict)
	pat := `(?im)^[ \t*_]*(?:(` + strings.Join(loose, "|") + `)[ \t*_]*(?::[ \t]*|[ \t]*\n)|(` + strings.Join(strict, "|") + `)[ \t*_]*:[ \t]*)`
	return regexp.MustCompile(pat), keys
}

var (
	quoteHeaderRe = regexp.MustCompile(`(?im)^\s*(On .{4,200}wrote:|-----\s*Original Message\s*-----|From: .+\nSent: .+)\s*$`)
	forwardRe     = regexp.MustCompile(`(?im)^[-\s]*(?:Forwarded message|Begin forwarded message:?)[-\s]*$`)
	fwdFromRe     = regexp.MustCompile(`(?im)^\s*From:\s*(.+)$`)
	fwdSubjectRe  = regexp.MustCompile(`(?im)^\s*Subject:\s*(.+)$`)
	urlRe         = regexp.MustCompile(`https?://[^\s<>"')\]]+`)
	emailRe       = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	yearRe        = regexp.MustCompile(`^(19|20)\d\d$`)
	injectionRe   = regexp.MustCompile(`(?i)ignore (all |any )?(previous|prior|above) instructions|disregard (the )?(previous|above)|you are (an? )?(ai|assistant|language model)|system prompt|mark (this|it|me) (as )?(approved|ready|qualified)|auto-?approve`)
	establishedRe = regexp.MustCompile(`(?i)(?:^|[.!]\s+)(?:an?|the)\s+(?:publicly[- ]traded|publicly[- ]listed|listed|multinational)\b|\b(?:we are|we're|our company is|the company is|is)\s+(?:a\s+|an\s+)?(?:publicly[- ]traded|publicly[- ]listed|listed (?:company|on)|multinational|conglomerate|fortune 500 company)|\blisted on the (?:NYSE|NASDAQ|NSE|BSE|SGX|LSE|CSE|DSE|PSX)\b|\b(?:established|founded|incorporated) in 1[89]\d\d\b|\bover \d{1,3},\d{3} employees\b|\b(?:investment fund|investment firm|private equity firm|family office)\b`)
	nonPitchRe    = regexp.MustCompile(`(?i)\b(newsletter|webinar|unsubscribe from this list|your (order|receipt|invoice)|invoice #|payment received|job application|applying for the (position|role)|my (cv|resume) (is )?attached|press release distribution|guest post|seo services|link building)\b`)
	testRe        = regexp.MustCompile(`(?i)\btest\b`)
	deckHostRe    = regexp.MustCompile(`(?i)docsend\.com|drive\.google\.com|docs\.google\.com/presentation|dropbox\.com|pitch\.com|canva\.com|notion\.(so|site)|\.pdf(\?|$)|\.pptx?(\?|$)|slideshare\.net|pitch\s?deck`)
	greetingRe    = regexp.MustCompile(`(?i)^\s*(hi|hello|hey|dear|greetings|good (morning|afternoon|evening))\b[^\n]{0,60}\n+`)
	signoffRe     = regexp.MustCompile(`(?im)^\s*(best|best regards|kind regards|warm regards|regards|thanks|thank you|cheers|sincerely)[,.!]?\s*$`)
	subjectNameRe = regexp.MustCompile(`(?i)^(?:(?:re|fwd?|fw)\s*:\s*)*(?:pitch(?:\s+deck)?|introducing|intro(?:duction)?|investment opportunity|fundraising)\s*[:\-–—|]\s*(.{2,80}?)\s*$|^(?:(?:re|fwd?|fw)\s*:\s*)*(.{2,60}?)\s*[\-–—|:]\s*(?:pitch(?: deck)?|seed round|pre-?seed|fundraising|investment opportunity)\b`)
)

// StripQuoted removes reply quotations so repeated content is not re-read.
// Forwarded messages are kept: their content is the pitch.
func StripQuoted(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if loc := quoteHeaderRe.FindStringIndex(text); loc != nil {
		fwd := forwardRe.FindStringIndex(text)
		if fwd == nil || fwd[0] > loc[0] {
			text = text[:loc[0]]
		}
	}
	var keep []string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), ">") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

// Pitch is a reviewable inbound submission. Every value is sender-reported.
type Pitch struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	HQ            string            `json:"hq"`
	Contact       string            `json:"contact"`
	Website       string            `json:"website"`
	Notes         string            `json:"notes"`
	Founder       string            `json:"founder,omitempty"`
	Stage         string            `json:"stage,omitempty"`
	Sector        string            `json:"sector,omitempty"`
	Incorporation string            `json:"incorporation,omitempty"`
	IncorpYear    string            `json:"incorporation_year,omitempty"`
	Sender        string            `json:"sender,omitempty"`
	ReplyTo       string            `json:"reply_to,omitempty"`
	ForwardedFrom string            `json:"forwarded_from,omitempty"`
	ContactSource string            `json:"contact_source,omitempty"`
	Subject       string            `json:"subject,omitempty"`
	Received      string            `json:"received,omitempty"`
	MessageID     string            `json:"message_id,omitempty"`
	ThreadID      string            `json:"thread_id,omitempty"`
	GmailURL      string            `json:"gmail_url,omitempty"`
	DeckLinks     []string          `json:"deck_links,omitempty"`
	Attachments   []Attachment      `json:"attachments,omitempty"`
	Fields        map[string]string `json:"fields,omitempty"`
	Structured    bool              `json:"structured"`
	NameGuessed   bool              `json:"name_guessed,omitempty"`
	Forwarded     bool              `json:"forwarded,omitempty"`
	Status        string            `json:"status"`
	Reasons       []string          `json:"reasons,omitempty"`
	Excerpt       string            `json:"excerpt,omitempty"`
	FollowUp      bool              `json:"follow_up,omitempty"`
	Demo          bool              `json:"demo,omitempty"`
	SheetRow      int               `json:"sheet_row,omitempty"`
	SheetRange    string            `json:"sheet_range,omitempty"`
}

// Input is an email reduced to the parts needed for extraction.
type Input struct {
	Text        string
	From        string
	ReplyTo     string
	Subject     string
	Date        string
	Unsubscribe bool
	Attachments []Attachment
}

// FromGmail builds extraction input from a Gmail API message.
func FromGmail(m GmailMessage) Input {
	plain, h, atts := Bodies(m.Payload)
	text := plain
	if strings.TrimSpace(text) == "" {
		text = HTMLToText(h)
	}
	date := m.Header("Date")
	if ms, err := strconv.ParseInt(m.InternalDate, 10, 64); err == nil && ms > 0 {
		date = time.UnixMilli(ms).UTC().Format(time.RFC3339)
	}
	return Input{Text: text, From: m.Header("From"), ReplyTo: m.Header("Reply-To"), Subject: m.Header("Subject"), Date: date, Unsubscribe: m.Header("List-Unsubscribe") != "", Attachments: atts}
}

func addr(s string) string {
	if a, err := mail.ParseAddress(strings.TrimSpace(s)); err == nil {
		return strings.ToLower(a.Address)
	}
	if m := emailRe.FindString(s); m != "" {
		return strings.ToLower(m)
	}
	return ""
}

func automated(a string) bool {
	return regexp.MustCompile(`(?i)^(no-?reply|do-?not-?reply|forms?|wordpress|notifications?|mailer|typeform|jotform|webflow|hello@hubspot)`).MatchString(a)
}

// ParseFields extracts labelled form fields. Values are verbatim.
func ParseFields(text string) map[string]string {
	fields := map[string]string{}
	ms := labelRe.FindAllStringSubmatchIndex(text, -1)
	for i, m := range ms {
		name := ""
		if m[2] >= 0 {
			name = text[m[2]:m[3]]
		} else {
			name = text[m[4]:m[5]]
		}
		end := len(text)
		if i+1 < len(ms) {
			end = ms[i+1][0]
		}
		v := strings.TrimSpace(text[m[1]:end])
		key := labelKeys[strings.ToLower(name)].key
		if key == "" || v == "" {
			continue
		}
		if _, ok := fields[key]; !ok {
			fields[key] = v
		}
	}
	return fields
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < 300 {
		s = s[:i]
	}
	return truncate(s, max)
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}

// Extract parses a pitch email. The inbound funnel is intentionally broad:
// no outbound geography, B2B or funding limits are applied here.
func Extract(in Input) Pitch {
	raw := strings.ReplaceAll(in.Text, "\r\n", "\n")
	p := Pitch{Sender: addr(in.From), ReplyTo: addr(in.ReplyTo), Subject: strings.TrimSpace(in.Subject), Received: in.Date, Attachments: in.Attachments, Status: "review"}
	text := StripQuoted(raw)
	if loc := forwardRe.FindStringIndex(text); loc != nil {
		p.Forwarded = true
		block := text[loc[1]:]
		if m := fwdFromRe.FindStringSubmatch(block[:min(len(block), 600)]); m != nil {
			p.ForwardedFrom = addr(m[1])
		}
		if m := fwdSubjectRe.FindStringSubmatch(block[:min(len(block), 600)]); m != nil && p.Subject == "" {
			p.Subject = strings.TrimSpace(m[1])
		}
	}
	isReply := regexp.MustCompile(`(?i)^\s*re\s*:`).MatchString(p.Subject)
	fields := ParseFields(text)
	p.Fields = fields
	p.Structured = fields["company"] != "" && fields["description"] != ""

	p.Name = truncate(firstLine(fields["company"], 300), 300)
	p.Description = truncate(strings.TrimSpace(fields["description"]), 12000)
	p.Founder = firstLine(fields["founder"], 200)
	p.Stage = firstLine(fields["stage"], 100)
	p.Sector = firstLine(fields["sector"], 200)
	p.Incorporation = firstLine(fields["incorporation"], 200)
	p.IncorpYear = firstLine(fields["incorporation_year"], 50)
	p.HQ = firstLine(fields["hq"], 300) // only an explicit HQ label or sentence; incorporation is never HQ
	if w := urlRe.FindString(fields["website"]); w != "" {
		p.Website = w
	} else if f := strings.TrimSpace(firstLine(fields["website"], 300)); f != "" && strings.Contains(f, ".") && !strings.Contains(f, " ") {
		p.Website = "https://" + strings.TrimPrefix(f, "//")
	}

	// Contact precedence: submitted contact > Reply-To > forwarded original sender > sender.
	switch {
	case emailRe.MatchString(fields["contact"]):
		p.Contact, p.ContactSource = strings.ToLower(emailRe.FindString(fields["contact"])), "form contact field"
	case p.ReplyTo != "" && p.ReplyTo != p.Sender:
		p.Contact, p.ContactSource = p.ReplyTo, "Reply-To header"
	case p.ForwardedFrom != "" && !automated(p.ForwardedFrom):
		p.Contact, p.ContactSource = p.ForwardedFrom, "forwarded original sender"
	case p.Sender != "" && !automated(p.Sender):
		p.Contact, p.ContactSource = p.Sender, "sender"
	}

	for _, u := range urlRe.FindAllString(text, -1) {
		u = strings.TrimRight(u, ".,;:")
		if deckHostRe.MatchString(u) && !contains(p.DeckLinks, u) {
			p.DeckLinks = append(p.DeckLinks, u)
		}
	}
	if d := firstLine(fields["deck"], 500); d != "" {
		if u := urlRe.FindString(d); u != "" && !contains(p.DeckLinks, u) {
			p.DeckLinks = append([]string{u}, p.DeckLinks...)
		}
	}

	if !p.Structured {
		body := strings.TrimSpace(text)
		if p.Forwarded {
			if loc := forwardRe.FindStringIndex(body); loc != nil {
				body = strings.TrimSpace(body[loc[1]:])
				body = regexp.MustCompile(`(?im)^\s*(From|Date|Sent|Subject|To|Cc):.*$`).ReplaceAllString(body, "")
				body = strings.TrimSpace(body)
			}
		}
		if p.Description == "" {
			desc := greetingRe.ReplaceAllString(body, "")
			if loc := signoffRe.FindAllStringIndex(desc, -1); len(loc) > 0 {
				desc = desc[:loc[len(loc)-1][0]]
			}
			p.Description = truncate(strings.TrimSpace(desc), 4000)
		}
		if p.Website == "" {
			for _, u := range urlRe.FindAllString(body, -1) {
				u = strings.TrimRight(u, ".,;:")
				if domain.IdentityDomain(u) != "" && !deckHostRe.MatchString(u) && !strings.Contains(strings.ToLower(u), "unsubscribe") {
					p.Website = u
					break
				}
			}
		}
	}
	// The company name is never guessed (from the subject, sender or domain):
	// if the email doesn't state it, it stays empty for a reviewer to fill in.
	p.Excerpt = truncate(strings.TrimSpace(text), 2000)
	hqQuote := ""
	if p.HQ == "" {
		// The sender's own explicit statement ("we're headquartered in
		// Colombo, Sri Lanka", "based in Dhaka") counts as stating HQ.
		// Markets, customers and incorporation never do.
		if m := hqSentenceRe.FindStringSubmatch(p.Description); m != nil {
			loc := m[2]
			if i := hqStopRe.FindStringIndex(loc); i != nil {
				loc = loc[:i[0]]
			}
			if loc = strings.Trim(strings.TrimSpace(loc), " ,"); loc != "" {
				p.HQ, hqQuote = truncate(loc, 120), strings.TrimSpace(m[0])
			}
		}
	}

	// Notes: verbatim extra fields plus explicit provenance caveats.
	var notes []string
	order := []struct{ k, label string }{{"founder", "Founder"}, {"stage", "Stage"}, {"sector", "Sector"}, {"incorporation", "Country of incorporation"}, {"incorporation_year", "Year of incorporation"}, {"location", "Location (sender-reported, not HQ)"}, {"raising", "Raising"}, {"funding", "Funding raised"}, {"revenue", "Revenue"}, {"phone", "Phone"}, {"linkedin", "LinkedIn"}, {"source", "Heard about us"}}
	for _, o := range order {
		if v := firstLine(fields[o.k], 500); v != "" {
			notes = append(notes, o.label+": "+v)
		}
	}
	if p.Website != "" {
		notes = append(notes, "Website: "+p.Website)
	}
	for _, d := range p.DeckLinks {
		notes = append(notes, "Deck (not reviewed): "+d)
	}
	for _, a := range p.Attachments {
		notes = append(notes, fmt.Sprintf("Attachment (not reviewed): %s", a.Filename))
	}
	if p.IncorpYear != "" && !yearRe.MatchString(strings.TrimSpace(p.IncorpYear)) {
		notes = append(notes, "Year of incorporation could not be parsed; kept as submitted.")
		p.Reasons = append(p.Reasons, "Unparseable incorporation year: "+p.IncorpYear)
	}
	if p.HQ == "" {
		notes = append(notes, "HQ unconfirmed; country of incorporation is not HQ.")
	} else if hqQuote != "" {
		notes = append(notes, "HQ from the sender's own words: \""+hqQuote+"\"")
	}
	notes = append(notes, "Sender-reported details; nothing verified by nVentures.")
	p.Notes = strings.Join(notes, "\n")

	classify(&p, in, isReply)
	return p
}

func classify(p *Pitch, in Input, isReply bool) {
	all := p.Name + "\n" + p.Description + "\n" + p.Subject
	ready := true
	add := func(r string) { p.Reasons = append(p.Reasons, r); ready = false }

	if injectionRe.MatchString(in.Text) {
		add("Contains instructions aimed at automated systems; they were ignored")
	}
	if domain.NameKey(p.Name) == "test" || domain.NameKey(p.Name) == "testcompany" || domain.NameKey(p.Name) == "ts" {
		if testRe.MatchString(p.Description) {
			p.Status = "skipped"
			p.Reasons = append(p.Reasons, "Test submission")
			return
		}
	}
	if !p.Structured {
		if in.Unsubscribe {
			p.Status = "skipped"
			p.Reasons = append(p.Reasons, "Bulk mail with List-Unsubscribe header and no pitch form")
			return
		}
		if nonPitchRe.MatchString(all) {
			p.Status = "skipped"
			p.Reasons = append(p.Reasons, "Not a startup pitch ("+strings.ToLower(nonPitchRe.FindString(all))+")")
			return
		}
		if isReply && len(strings.TrimSpace(p.Description)) < 400 && len(p.DeckLinks) == 0 && len(p.Attachments) == 0 {
			p.FollowUp = true
			add("Reply in an existing conversation; check whether it introduces a new company")
		}
		add("Unstructured email: confirm company name, description and contact")
	}
	if strings.TrimSpace(p.Name) == "" {
		add("Company name not stated; add it before approving")
	}
	if len(strings.TrimSpace(p.Description)) < 35 {
		add("Description too thin to assess")
	}
	if !strings.Contains(p.Contact, "@") {
		add("No contact email found")
	}
	if establishedRe.MatchString(p.Description) {
		add("Possible established/public business or investor: " + establishedRe.FindString(p.Description))
	}
	if len(p.Reasons) > 0 {
		ready = false
	}
	if ready {
		// A complete pitch is added to the sheet automatically only when it
		// also fits nVentures' focus as stated in the email. Anything else
		// waits for a person in Needs review; nothing is rejected for region
		// or stage (the inbound funnel stays broad).
		if gaps := FocusGaps(*p); len(gaps) > 0 {
			ready = false
			p.Reasons = append(p.Reasons, "Not added automatically: "+strings.Join(gaps, "; "))
		}
	}
	if ready {
		p.Status = "ready"
	} else {
		p.Status = "review"
	}
}

var (
	hqSentenceRe = regexp.MustCompile(`(?i)\b(headquartered in|headquarters (?:is |are )?in|hq (?:is )?in|hq:|based (?:in|out of))\s+([^.;\n]{2,80})`)
	hqStopRe     = regexp.MustCompile(`(?i)\b(and|with|where|serving|serves|selling|for|since|but|while|who|which)\b`)
	earlyStageRe = regexp.MustCompile(`(?i)\b(pre-?seed|seed|angel|bootstrapped|self-funded)\b`)
	b2bSectorRe  = regexp.MustCompile(`(?i)\b(b2b|saas|enterprise|smes?|msmes?)\b`)
)

// FocusGaps lists what keeps a complete pitch from being added
// automatically: a website, an explicitly stated HQ in South Asia or
// Singapore, and a stated B2B focus or pre-seed/seed stage. Incorporation
// country is not HQ; nothing is inferred.
func FocusGaps(p Pitch) []string {
	var gaps []string
	if strings.TrimSpace(p.Website) == "" {
		gaps = append(gaps, "no website stated")
	}
	if strings.TrimSpace(p.HQ) == "" {
		gaps = append(gaps, "HQ not stated (incorporation country doesn't count)")
	} else if !HQInFocus(p.HQ) {
		gaps = append(gaps, "HQ outside South Asia/Singapore ("+p.HQ+")")
	}
	b2b := b2bSectorRe.MatchString(p.Sector) || (b2bRe.MatchString(p.Description) && !b2cRe.MatchString(p.Description))
	if !b2b && !earlyStageRe.MatchString(p.Stage) {
		gaps = append(gaps, "no B2B focus or pre-seed/seed stage stated")
	}
	return gaps
}

// HQInFocus reports whether a stated HQ names one of the focus countries.
func HQInFocus(hq string) bool {
	for _, c := range domain.Countries {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(c) + `\b`).MatchString(hq) {
			return true
		}
	}
	return false
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

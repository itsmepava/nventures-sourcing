package google

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
)

// Gmail is a read-only Gmail API client. It has no method that sends,
// modifies, labels, archives or deletes mail.
type Gmail struct {
	Base   string
	HTTP   *http.Client
	Tokens TokenSource
	Policy netx.Policy
}

func (g *Gmail) get(ctx context.Context, path string, out any) error {
	tok, err := g.Tokens.Token(ctx)
	if err != nil {
		return err
	}
	p := g.Policy
	if p.Attempts == 0 {
		p = netx.DefaultRead
	}
	return netx.DoJSON(ctx, g.HTTP, http.MethodGet, g.Base+"/gmail/v1/users/me/"+path, map[string]string{"Authorization": "Bearer " + tok}, nil, out, p)
}

// Profile returns the connected mailbox address.
func (g *Gmail) Profile(ctx context.Context) (string, error) {
	var out struct {
		EmailAddress string `json:"emailAddress"`
	}
	err := g.get(ctx, "profile", &out)
	return out.EmailAddress, err
}

// MessageRef is a listed message.
type MessageRef struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

// List returns one page of message IDs matching q.
func (g *Gmail) List(ctx context.Context, q, pageToken string, max int) ([]MessageRef, string, error) {
	v := url.Values{"q": {q}, "maxResults": {strconv.Itoa(max)}}
	if pageToken != "" {
		v.Set("pageToken", pageToken)
	}
	var out struct {
		Messages      []MessageRef `json:"messages"`
		NextPageToken string       `json:"nextPageToken"`
	}
	err := g.get(ctx, "messages?"+v.Encode(), &out)
	return out.Messages, out.NextPageToken, err
}

// Get fetches a full message.
func (g *Gmail) Get(ctx context.Context, id string) (extract.GmailMessage, error) {
	var m extract.GmailMessage
	err := g.get(ctx, "messages/"+url.PathEscape(id)+"?format=full", &m)
	return m, err
}

// Sheets is a minimal Google Sheets values client.
type Sheets struct {
	Base   string
	HTTP   *http.Client
	Tokens TokenSource
	Policy netx.Policy
}

func (s *Sheets) do(ctx context.Context, method, path string, in, out any, p netx.Policy) error {
	tok, err := s.Tokens.Token(ctx)
	if err != nil {
		return err
	}
	return netx.DoJSON(ctx, s.HTTP, method, s.Base+"/v4/spreadsheets/"+path, map[string]string{"Authorization": "Bearer " + tok}, in, out, p)
}

func (s *Sheets) read() netx.Policy {
	if s.Policy.Attempts == 0 {
		return netx.DefaultRead
	}
	return s.Policy
}

// QuoteTab returns an A1 sheet name reference.
func QuoteTab(tab string) string {
	return "'" + strings.ReplaceAll(tab, "'", "''") + "'"
}

// Values reads a range as strings.
func (s *Sheets) Values(ctx context.Context, id, rng string) ([][]string, error) {
	var out struct {
		Values [][]any `json:"values"`
	}
	if err := s.do(ctx, http.MethodGet, url.PathEscape(id)+"/values/"+url.PathEscape(rng), nil, &out, s.read()); err != nil {
		return nil, err
	}
	rows := make([][]string, len(out.Values))
	for i, r := range out.Values {
		rows[i] = make([]string, len(r))
		for j, c := range r {
			if str, ok := c.(string); ok {
				rows[i][j] = str
			} else if c != nil {
				rows[i][j] = fmt.Sprint(c)
			}
		}
	}
	return rows, nil
}

// Tabs lists tab titles (connection test, no writes).
func (s *Sheets) Tabs(ctx context.Context, id string) ([]string, error) {
	var out struct {
		Sheets []struct {
			Properties struct {
				Title string `json:"title"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	if err := s.do(ctx, http.MethodGet, url.PathEscape(id)+"?fields=sheets.properties.title", nil, &out, s.read()); err != nil {
		return nil, err
	}
	var tabs []string
	for _, x := range out.Sheets {
		tabs = append(tabs, x.Properties.Title)
	}
	return tabs, nil
}

// Append adds one row with RAW input exactly once and returns updatedRange.
// It is never retried automatically: a failure may still have written.
func (s *Sheets) Append(ctx context.Context, id, tab string, row []string) (string, error) {
	var out struct {
		Updates struct {
			UpdatedRange string `json:"updatedRange"`
		} `json:"updates"`
	}
	path := url.PathEscape(id) + "/values/" + url.PathEscape(QuoteTab(tab)+"!A1") + ":append?valueInputOption=RAW&insertDataOption=INSERT_ROWS&includeValuesInResponse=false"
	if err := s.do(ctx, http.MethodPost, path, map[string]any{"values": [][]string{row}, "majorDimension": "ROWS"}, &out, netx.WriteOnce); err != nil {
		return "", err
	}
	return out.Updates.UpdatedRange, nil
}

// Update overwrites a specific range with RAW values (partner control tab only).
func (s *Sheets) Update(ctx context.Context, id, rng string, row []string) error {
	return s.do(ctx, http.MethodPut, url.PathEscape(id)+"/values/"+url.PathEscape(rng)+"?valueInputOption=RAW", map[string]any{"values": [][]string{row}, "majorDimension": "ROWS"}, nil, netx.WriteOnce)
}

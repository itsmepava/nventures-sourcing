// Package google talks to Gmail (read-only) and Google Sheets through
// net/http. It never sends, modifies, labels or deletes mail.
package google

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/netx"
)

// Scopes requested by the app. gmail.readonly cannot send or modify mail.
const (
	ScopeGmailRead = "https://www.googleapis.com/auth/gmail.readonly"
	ScopeSheets    = "https://www.googleapis.com/auth/spreadsheets"
)

// Endpoints locates Google services (replaced by the fixture server in demo).
type Endpoints struct {
	Gmail, Sheets, Token, Auth string
}

// LiveEndpoints are Google's production endpoints.
var LiveEndpoints = Endpoints{Gmail: "https://gmail.googleapis.com", Sheets: "https://sheets.googleapis.com", Token: "https://oauth2.googleapis.com/token", Auth: "https://accounts.google.com/o/oauth2/v2/auth"}

// TokenSource yields bearer tokens.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type cached struct {
	mu     sync.Mutex
	token  string
	expiry time.Time
	fetch  func(ctx context.Context) (string, time.Duration, error)
}

func (c *cached) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expiry) > time.Minute {
		return c.token, nil
	}
	t, ttl, err := c.fetch(ctx)
	if err != nil {
		return "", err
	}
	c.token, c.expiry = t, time.Now().Add(ttl)
	return t, nil
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func postForm(ctx context.Context, hc *http.Client, endpoint string, form url.Values) (tokenResp, error) {
	var tr tokenResp
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tr, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return tr, &netx.ProviderError{Msg: "Could not reach Google authorization server"}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(body, &tr)
	if resp.StatusCode != 200 || tr.AccessToken == "" {
		if tr.Error == "invalid_grant" {
			return tr, &netx.ProviderError{Msg: "Google authorization expired or was revoked. Reconnect Google in Settings.", Status: resp.StatusCode}
		}
		return tr, &netx.ProviderError{Msg: "Google authorization failed. Check the OAuth client and reconnect.", Status: resp.StatusCode}
	}
	return tr, nil
}

// RefreshTokenSource exchanges an OAuth refresh token for access tokens.
func RefreshTokenSource(hc *http.Client, tokenURL, clientID, secret, refresh string) TokenSource {
	return &cached{fetch: func(ctx context.Context) (string, time.Duration, error) {
		tr, err := postForm(ctx, hc, tokenURL, url.Values{"client_id": {clientID}, "client_secret": {secret}, "refresh_token": {refresh}, "grant_type": {"refresh_token"}})
		if err != nil {
			return "", 0, err
		}
		return tr.AccessToken, time.Duration(max(tr.ExpiresIn, 60)) * time.Second, nil
	}}
}

// ServiceAccount is the subset of a Google service-account key file.
type ServiceAccount struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// ParseServiceAccount accepts raw JSON or base64 JSON (legacy env format).
func ParseServiceAccount(raw string) (*ServiceAccount, *rsa.PrivateKey, error) {
	raw = strings.TrimSpace(raw)
	var sa ServiceAccount
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		dec, derr := base64.StdEncoding.DecodeString(raw)
		if derr != nil || json.Unmarshal(dec, &sa) != nil {
			return nil, nil, errors.New("service account JSON is not valid JSON or base64 JSON")
		}
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(sa.PrivateKey, `\n`, "\n")))
	if block == nil || sa.ClientEmail == "" {
		return nil, nil, errors.New("service account key is incomplete")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if k2, err2 := x509.ParsePKCS1PrivateKey(block.Bytes); err2 == nil {
			return &sa, k2, nil
		}
		return nil, nil, errors.New("service account private key could not be parsed")
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("service account key is not RSA")
	}
	return &sa, rk, nil
}

// ServiceAccountTokenSource signs a JWT bearer assertion (RFC 7523).
func ServiceAccountTokenSource(hc *http.Client, tokenURL string, sa *ServiceAccount, key *rsa.PrivateKey, scopes ...string) TokenSource {
	return &cached{fetch: func(ctx context.Context) (string, time.Duration, error) {
		now := time.Now()
		enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
		head := enc(map[string]string{"alg": "RS256", "typ": "JWT"})
		claims := enc(map[string]any{"iss": sa.ClientEmail, "scope": strings.Join(scopes, " "), "aud": tokenURL, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
		sum := sha256.Sum256([]byte(head + "." + claims))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		if err != nil {
			return "", 0, err
		}
		tr, err := postForm(ctx, hc, tokenURL, url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {head + "." + claims + "." + base64.RawURLEncoding.EncodeToString(sig)}})
		if err != nil {
			return "", 0, err
		}
		return tr.AccessToken, time.Duration(max(tr.ExpiresIn, 60)) * time.Second, nil
	}}
}

// StaticToken is used in tests.
type StaticToken string

// Token returns the static token.
func (s StaticToken) Token(context.Context) (string, error) { return string(s), nil }

// ---- interactive desktop OAuth (PKCE) ----

// ClientConfig is an OAuth Desktop/Web client from Google Cloud Console.
type ClientConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// ParseClientJSON reads a downloaded client_secret JSON file.
func ParseClientJSON(b []byte) (ClientConfig, error) {
	var wrap struct {
		Installed *ClientConfig `json:"installed"`
		Web       *ClientConfig `json:"web"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return ClientConfig{}, errors.New("not a Google OAuth client JSON file")
	}
	c := wrap.Installed
	if c == nil {
		c = wrap.Web
	}
	if c == nil || c.ClientID == "" {
		return ClientConfig{}, errors.New("client JSON must contain an installed or web client_id")
	}
	return *c, nil
}

// PKCE holds per-attempt verifier/state.
type PKCE struct {
	Verifier, Challenge, State string
	Created                    time.Time
}

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewPKCE creates a verifier, S256 challenge and state.
func NewPKCE() PKCE {
	v := randToken(48)
	sum := sha256.Sum256([]byte(v))
	return PKCE{Verifier: v, Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), State: randToken(24), Created: time.Now()}
}

// AuthURL builds the consent URL.
func AuthURL(authEndpoint string, c ClientConfig, redirect, loginHint string, p PKCE) string {
	q := url.Values{"client_id": {c.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {ScopeGmailRead + " " + ScopeSheets},
		"access_type": {"offline"}, "prompt": {"consent"}, "state": {p.State}, "code_challenge": {p.Challenge}, "code_challenge_method": {"S256"}}
	if loginHint != "" {
		q.Set("login_hint", loginHint)
	}
	return authEndpoint + "?" + q.Encode()
}

// Exchange trades an authorization code for tokens.
func Exchange(ctx context.Context, hc *http.Client, tokenURL string, c ClientConfig, redirect, code string, p PKCE) (refresh, access string, err error) {
	tr, err := postForm(ctx, hc, tokenURL, url.Values{"client_id": {c.ClientID}, "client_secret": {c.ClientSecret}, "code": {code}, "code_verifier": {p.Verifier}, "redirect_uri": {redirect}, "grant_type": {"authorization_code"}})
	if err != nil {
		return "", "", err
	}
	if tr.RefreshToken == "" {
		return "", "", &netx.ProviderError{Msg: "Google did not return a refresh token; remove the app's access in your Google account and connect again"}
	}
	if !strings.Contains(tr.Scope, ScopeGmailRead) {
		return "", "", &netx.ProviderError{Msg: "Gmail read-only permission was not granted"}
	}
	return tr.RefreshToken, tr.AccessToken, nil
}

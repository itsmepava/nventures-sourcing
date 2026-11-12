package httpapp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/itsmepava/nventures-sourcing/internal/config"
)

func newTestApp(t *testing.T, mutate func(*config.Config)) (*App, *httptest.Server) {
	t.Helper()
	cfg := config.Config{Mode: "demo", DataDir: t.TempDir(), Host: "127.0.0.1", MaxResearch: 45}
	if mutate != nil {
		mutate(&cfg)
	}
	a, err := New(cfg, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler())
	a.Port = srv.Listener.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() { srv.Close(); a.Close() })
	return a, srv
}

func do(t *testing.T, c *http.Client, method, url string, hdr map[string]string, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestHostOriginAndHeaderProtection(t *testing.T) {
	_, srv := newTestApp(t, nil)
	c := srv.Client()
	ok := map[string]string{"X-NV-Request": "1", "Content-Type": "application/json"}
	if r := do(t, c, "GET", srv.URL+"/api/state", map[string]string{"Host": "attacker.example"}, ""); r.StatusCode != 403 {
		t.Errorf("rebinding host allowed: %d", r.StatusCode)
	}
	if r := do(t, c, "POST", srv.URL+"/api/partners", map[string]string{"Content-Type": "application/json"}, `{"name":"X Fund"}`); r.StatusCode != 403 {
		t.Errorf("missing header allowed: %d", r.StatusCode)
	}
	if r := do(t, c, "POST", srv.URL+"/api/partners", map[string]string{"X-NV-Request": "1", "Origin": "http://evil.example"}, `{"name":"X Fund"}`); r.StatusCode != 403 {
		t.Errorf("cross origin allowed: %d", r.StatusCode)
	}
	if r := do(t, c, "POST", srv.URL+"/api/partners", ok, `{"name":"Test Fund","website":"https://fund.example"}`); r.StatusCode != 201 {
		t.Errorf("same-origin request refused: %d", r.StatusCode)
	}
	r := do(t, c, "GET", srv.URL+"/", nil, "")
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "default-src 'self'") || r.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("security headers missing")
	}
	if r := do(t, c, "POST", srv.URL+"/api/settings", ok, `{"inbound_sheet":"x"}`); r.StatusCode != 400 {
		t.Error("demo destinations editable")
	}
	if r := do(t, c, "POST", srv.URL+"/api/partners", ok, `{"name":"Bad","website":"javascript:alert(1)"}`); r.StatusCode != 400 {
		t.Error("javascript URL accepted")
	}
}

func TestAccessTokenRequiredForRemoteBind(t *testing.T) {
	token := "0123456789abcdef0123456789abcdef"
	_, srv := newTestApp(t, func(c *config.Config) { c.AccessToken = token })
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if r := do(t, c, "GET", srv.URL+"/api/state", nil, ""); r.StatusCode != 401 {
		t.Fatalf("unauthenticated API: %d", r.StatusCode)
	}
	if r := do(t, c, "GET", srv.URL+"/health", nil, ""); r.StatusCode != 200 {
		t.Error("health should stay public for probes")
	}
	do(t, c, "POST", srv.URL+"/login", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, "token=wrong")
	if r := do(t, c, "GET", srv.URL+"/api/state", nil, ""); r.StatusCode != 401 {
		t.Error("wrong token accepted")
	}
	do(t, c, "POST", srv.URL+"/login", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, "token="+token)
	if r := do(t, c, "GET", srv.URL+"/api/state", nil, ""); r.StatusCode != 200 {
		t.Errorf("session not established: %d", r.StatusCode)
	}
}

func TestDemoOAuthConsentFlowWithPKCE(t *testing.T) {
	a, srv := newTestApp(t, nil)
	c := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if strings.Contains(req.URL.Host, "127.0.0.1") && strings.HasPrefix(req.URL.Path, "/oauth/callback") {
			req.URL.Host = strings.TrimPrefix(srv.URL, "http://") // same server
		}
		return nil
	}}
	r := do(t, c, "GET", srv.URL+"/oauth/start", nil, "")
	if !strings.Contains(r.Request.URL.Fragment, "google=connected") {
		t.Fatalf("consent flow did not complete: %s", r.Request.URL)
	}
	// Replaying an old state is refused.
	r = do(t, c, "GET", srv.URL+"/oauth/callback?state=forged&code=x", nil, "")
	if !strings.Contains(r.Request.URL.Fragment, "google=error") {
		t.Errorf("forged state accepted: %s", r.Request.URL)
	}
	evs, _ := a.Store.Events(t.Context(), 0, "google_connected", 0, 5)
	if len(evs) != 1 {
		t.Error("connection not audited")
	}
}

func TestCSVFormulaEscaping(t *testing.T) {
	for in, want := range map[string]string{"=SUM(A1)": "'=SUM(A1)", "+1": "'+1", "-2": "'-2", "@x": "'@x", "\tx": "'\tx", "plain": "plain", "": ""} {
		if got := SafeCell(in); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}

func TestInstanceLockAndInterruptedRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{Mode: "demo", DataDir: dir, Host: "127.0.0.1", MaxResearch: 45}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := New(cfg, "test", log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg, "test", log); err == nil {
		t.Fatal("second instance on the same data directory allowed")
	}
	j, _ := a.Store.CreateJob(t.Context(), "inbound_import", "demo", nil)
	a.Close()
	b, err := New(cfg, "test", log)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got, _ := b.Store.Job(t.Context(), j.ID)
	if got.Status != "interrupted" {
		t.Errorf("job after restart: %s", got.Status)
	}
}

func TestStateNeverExposesSecrets(t *testing.T) {
	secret := "super-secret-client-value-123"
	_, srv := newTestApp(t, func(c *config.Config) { c.OAuthClientSecret = secret; c.OAuthRefreshToken = "refresh-" + secret })
	r := do(t, srv.Client(), "GET", srv.URL+"/api/state", nil, "")
	b, _ := io.ReadAll(r.Body)
	if strings.Contains(string(b), secret) {
		t.Fatal("secret leaked in /api/state")
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		t.Fatal("bad json")
	}
}

func TestDeterministicRehearsal(t *testing.T) {
	rep, err := Rehearse(t.TempDir(), "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed {
		t.Fatalf("rehearsal failures: %v", rep.Failures)
	}
}

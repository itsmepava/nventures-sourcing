package httpapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/itsmepava/nventures-sourcing/web"
)

// Handler builds the HTTP handler with all routes and protections.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(web.FS, "static")
	files := http.FileServer(http.FS(static))
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(files)))
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/brand/icon-32.png", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /readyz", a.readyz)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)

	mux.HandleFunc("GET /api/state", a.state)
	mux.HandleFunc("GET /api/dashboard", a.dashboard)
	mux.HandleFunc("GET /api/settings", a.getSettings)
	mux.HandleFunc("POST /api/settings", a.postSettings)
	mux.HandleFunc("POST /api/preferences", a.postPreferences)

	mux.HandleFunc("GET /api/pitches", a.listPitches)
	mux.HandleFunc("GET /api/pitches/export.csv", a.exportPitches)
	mux.HandleFunc("GET /api/pitches/{id}", a.getPitch)
	mux.HandleFunc("POST /api/pitches/{id}", a.reviewPitch)
	mux.HandleFunc("POST /api/pitches/{id}/star", a.star("pitches"))
	mux.HandleFunc("POST /api/pitches/{id}/suggest", a.suggestPitch)
	mux.HandleFunc("POST /api/inbox/import", a.startJob("inbound_import"))
	mux.HandleFunc("POST /api/inbox/sync", a.startJob("inbound_sync"))

	mux.HandleFunc("GET /api/companies", a.listCompanies)
	mux.HandleFunc("GET /api/companies/export.csv", a.exportCompanies)
	mux.HandleFunc("GET /api/companies/{id}", a.getCompany)
	mux.HandleFunc("POST /api/companies/{id}/star", a.star("companies"))
	mux.HandleFunc("POST /api/sourcing/run", a.startJob("outbound_run"))
	mux.HandleFunc("POST /api/sourcing/sync", a.startJob("outbound_sync"))
	mux.HandleFunc("POST /api/sourcing/research", a.startJob("outbound_research"))
	mux.HandleFunc("GET /api/candidates", a.listCandidates)
	mux.HandleFunc("GET /api/candidates/{id}", a.getCandidate)

	mux.HandleFunc("GET /api/partners", a.listPartners)
	mux.HandleFunc("POST /api/partners", a.addPartner)
	mux.HandleFunc("PATCH /api/partners/{id}", a.updatePartner)

	mux.HandleFunc("GET /api/jobs", a.listJobs)
	mux.HandleFunc("GET /api/jobs/{id}", a.getJob)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", a.cancelJob)
	mux.HandleFunc("GET /api/events", a.listEvents)
	mux.HandleFunc("GET /api/sync-ops", a.listSyncOps)

	mux.HandleFunc("POST /api/google/client", a.uploadClient)
	mux.HandleFunc("POST /api/google/disconnect", a.disconnect)
	mux.HandleFunc("POST /api/google/test", a.testConnection)
	mux.HandleFunc("GET /oauth/start", a.oauthStart)
	mux.HandleFunc("GET /oauth/callback", a.oauthCallback)
	mux.HandleFunc("GET /api/llm", a.llmStatus)
	mux.HandleFunc("POST /api/cache/clear", a.clearCache)
	mux.HandleFunc("POST /api/demo/reset", a.demoReset)
	mux.HandleFunc("GET /demo-site/{host}/{path...}", a.demoSite)
	return a.protect(mux)
}

func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

// ---- protections ----

func (a *App) allowedHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.Trim(strings.ToLower(h), "[]")
	if h == "127.0.0.1" || h == "localhost" || h == "::1" {
		return true
	}
	if a.Cfg.BaseURL != "" {
		if u, err := url.Parse(a.Cfg.BaseURL); err == nil && strings.EqualFold(u.Hostname(), h) {
			return true
		}
	}
	if !a.Cfg.Loopback() && a.Cfg.AccessToken != "" && a.Cfg.Host != "0.0.0.0" && a.Cfg.Host != "::" && h == strings.ToLower(a.Cfg.Host) {
		return true
	}
	return false
}

func (a *App) sessionValue() string {
	m := hmac.New(sha256.New, []byte(a.Cfg.AccessToken))
	m.Write([]byte("nventures-session-v1"))
	return hex.EncodeToString(m.Sum(nil))
}

func (a *App) authenticated(r *http.Request) bool {
	if a.Cfg.AccessToken == "" {
		return true
	}
	if c, err := r.Cookie("nv_session"); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.sessionValue())) == 1 {
		return true
	}
	auth := r.Header.Get("Authorization")
	return strings.HasPrefix(auth, "Bearer ") && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(a.Cfg.AccessToken)) == 1
}

func (a *App) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if !strings.HasPrefix(r.URL.Path, "/demo-site/") {
			h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		}
		defer func() {
			if p := recover(); p != nil {
				a.Log.Error("handler panic", "path", r.URL.Path, "panic", fmt.Sprint(p))
				writeErr(w, 500, "Internal error")
			}
		}()
		// DNS-rebinding protection: only expected Host headers.
		if !a.allowedHost(r.Host) {
			http.Error(w, "Host not allowed", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Cross-site request protection: same-origin only plus a custom header
			// that a cross-origin form cannot send without a CORS preflight.
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || !a.allowedHost(u.Host) || !strings.EqualFold(u.Host, r.Host) {
					http.Error(w, "Origin not allowed", http.StatusForbidden)
					return
				}
			}
			if r.URL.Path != "/login" && r.Header.Get("X-NV-Request") != "1" {
				http.Error(w, "Missing request header", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		}
		if !a.authenticated(r) && r.URL.Path != "/login" && r.URL.Path != "/health" && !strings.HasPrefix(r.URL.Path, "/static/") {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeErr(w, 401, "Sign in required")
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}
		sw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, r)
		if !strings.HasPrefix(r.URL.Path, "/static/") && r.URL.Path != "/api/jobs" && !strings.HasPrefix(r.URL.Path, "/api/jobs/") {
			a.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.code, "ms", time.Since(start).Milliseconds())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return errors.New("invalid JSON body")
	}
	return nil
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func qInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

// ---- pages ----

var indexTmpl = template.Must(template.ParseFS(web.FS, "templates/index.html"))

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = indexTmpl.Execute(w, map[string]string{"Version": a.Version, "Mode": a.Cfg.Mode})
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok", "app": "nventures", "version": a.Version, "mode": a.Cfg.Mode, "uptime_seconds": int(time.Since(a.Started).Seconds())})
}

func (a *App) readyz(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, 503, "database unavailable")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ready", "schema": a.Store.SchemaVersion(r.Context())})
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>nVentures sign in</title><link rel="stylesheet" href="/static/app.css"></head><body class="login"><main class="login-card"><img src="/static/brand/logo.png" alt="nVentures" width="200"><h1>Sign in</h1><form method="post" action="/login"><label>Access token<input name="token" type="password" autocomplete="current-password" required></label><button class="btn primary" type="submit">Sign in</button></form></main></body></html>`)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.AccessToken == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("token")), []byte(a.Cfg.AccessToken)) != 1 {
		time.Sleep(500 * time.Millisecond)
		http.Redirect(w, r, "/login?failed=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "nv_session", Value: a.sessionValue(), Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: strings.HasPrefix(a.Cfg.BaseURL, "https://"), MaxAge: 12 * 3600})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

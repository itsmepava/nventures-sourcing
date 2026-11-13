package httpapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/auth"
	"github.com/itsmepava/nventures-sourcing/internal/store"
	"github.com/itsmepava/nventures-sourcing/web"
)

// The backend is a JSON API. The frontend (web/src) is a separate static
// application that only talks to /api; it is embedded into the binary for
// one-command use, or served from disk with NV_WEB_DIR during development.

type route struct {
	pattern string
	perm    string // "" = public, auth.View = any signed-in user
	h       http.HandlerFunc
}

func (a *App) routes() []route {
	V := auth.View
	return []route{
		{"GET /api/health", "", a.health},
		{"GET /health", "", a.health},
		{"GET /readyz", "", a.readyz},
		{"GET /api/auth/state", "", a.authState},
		{"POST /api/auth/setup", "", a.authSetup},
		{"POST /api/auth/login", "", a.authLogin},
		{"POST /api/auth/logout", "", a.authLogout},

		{"GET /api/me", V, a.me},
		{"POST /api/me/password", V, a.changePassword},
		{"GET /api/state", V, a.state},
		{"GET /api/dashboard", V, a.dashboard},
		{"GET /api/settings", V, a.getSettings},
		{"POST /api/settings", auth.SettingsManage, a.postSettings},
		{"POST /api/preferences", V, a.postPreferences},

		{"GET /api/pitches", V, a.listPitches},
		{"GET /api/pitches/export.csv", V, a.exportPitches},
		{"GET /api/pitches/{id}", V, a.getPitch},
		{"POST /api/pitches/{id}", auth.InboxReview, a.reviewPitch},
		{"POST /api/pitches/{id}/star", auth.Shortlist, a.star("pitches")},
		{"POST /api/pitches/{id}/workflow", auth.PitchWork, a.setWorkflow("pitches")},
		{"POST /api/pitches/{id}/suggest", auth.InboxReview, a.suggestPitch},
		{"POST /api/inbox/import", auth.InboxImport, a.startJob("inbound_import")},
		{"POST /api/inbox/sync", auth.InboxSync, a.startJob("inbound_sync")},

		{"GET /api/companies", V, a.listCompanies},
		{"GET /api/companies/export.csv", V, a.exportCompanies},
		{"GET /api/companies/{id}", V, a.getCompany},
		{"POST /api/companies/{id}/star", auth.Shortlist, a.star("companies")},
		{"POST /api/companies/{id}/workflow", auth.CompanyWork, a.setWorkflow("companies")},
		{"POST /api/sourcing/run", auth.SourcingRun, a.startJob("outbound_run")},
		{"POST /api/sourcing/sync", auth.SourcingSync, a.startJob("outbound_sync")},
		{"POST /api/sourcing/research", auth.SourcingRun, a.startJob("outbound_research")},
		{"GET /api/candidates", V, a.listCandidates},
		{"GET /api/candidates/{id}", V, a.getCandidate},

		{"GET /api/partners", V, a.listPartners},
		{"POST /api/partners", auth.PartnersEdit, a.addPartner},
		{"PATCH /api/partners/{id}", auth.PartnersEdit, a.updatePartner},

		{"GET /api/jobs", V, a.listJobs},
		{"GET /api/jobs/{id}", V, a.getJob},
		{"POST /api/jobs/{id}/cancel", auth.JobsControl, a.cancelJob},
		{"POST /api/jobs/{id}/retry", auth.JobsControl, a.retryJob},
		{"GET /api/related", V, a.related},
		{"GET /api/events", V, a.listEvents},
		{"GET /api/sync-ops", V, a.listSyncOps},

		{"GET /api/notes", V, a.listNotes},
		{"POST /api/notes", V, a.addNote}, // entity-specific permission checked inside
		{"GET /api/queue", V, a.queue},
		{"GET /api/notifications", V, a.listNotifications},
		{"POST /api/notifications/read", V, a.readNotifications},
		{"GET /api/team", V, a.teamBrief},
		{"GET /api/users", auth.UsersManage, a.listUsers},
		{"POST /api/users", auth.UsersManage, a.createUser},
		{"PATCH /api/users/{id}", auth.UsersManage, a.updateUser},
		{"POST /api/users/{id}/reset-password", auth.UsersManage, a.resetPassword},
		{"GET /api/templates", V, a.listTemplates},
		{"POST /api/templates", auth.TemplatesManage, a.saveTemplate},
		{"PUT /api/templates/{id}", auth.TemplatesManage, a.saveTemplate},
		{"DELETE /api/templates/{id}", auth.TemplatesManage, a.deleteTemplate},
		{"GET /api/schedules", auth.SchedulesManage, a.listSchedules},
		{"POST /api/schedules", auth.SchedulesManage, a.saveSchedule},
		{"PUT /api/schedules/{id}", auth.SchedulesManage, a.saveSchedule},
		{"DELETE /api/schedules/{id}", auth.SchedulesManage, a.deleteSchedule},
		{"POST /api/schedules/{id}/run", auth.SchedulesManage, a.runScheduleNow},

		{"POST /api/google/client", auth.SettingsManage, a.uploadClient},
		{"POST /api/google/disconnect", auth.SettingsManage, a.disconnect},
		{"POST /api/google/test", auth.SettingsManage, a.testConnection},
		{"GET /oauth/start", auth.SettingsManage, a.oauthStart},
		{"GET /oauth/callback", auth.SettingsManage, a.oauthCallback},
		{"GET /api/llm", V, a.llmStatus},
		{"POST /api/cache/clear", auth.SettingsManage, a.clearCache},
		{"POST /api/demo/reset", auth.DemoReset, a.demoReset},
		{"GET /api/demo/sheet", V, a.demoSheet},
		{"GET /demo-site/{host}/{path...}", V, a.demoSite},
	}
}

// Handler builds the HTTP handler with all routes and protections.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range a.routes() {
		mux.HandleFunc(rt.pattern, a.guard(rt.perm, rt.h))
	}
	mux.Handle("GET /", a.frontend())
	return a.protect(mux)
}

// ---- frontend static files ----

type asset struct {
	body  []byte
	ctype string
	etag  string
}

// frontend serves the separate web application. Embedded files get content
// ETags so an upgraded binary never serves stale assets.
func (a *App) frontend() http.Handler {
	if dir := os.Getenv("NV_WEB_DIR"); dir != "" {
		files := http.FileServer(http.Dir(dir))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		})
	}
	src, _ := fs.Sub(web.FS, "src")
	assets := map[string]asset{}
	_ = fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		ct := mime.TypeByExtension(path.Ext(p))
		if ct == "" {
			ct = "application/octet-stream"
		}
		assets["/"+p] = asset{body: b, ctype: ct, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		return nil
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" {
			p = "/index.html"
		}
		if p == "/favicon.ico" {
			p = "/assets/brand/icon-32.png"
		}
		f, ok := assets[p]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", f.ctype)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", f.etag)
		if r.Header.Get("If-None-Match") == f.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(f.body)
	})
}

// ---- sessions and permissions ----

const sessionCookie = "nv_sid"
const sessionTTL = 7 * 24 * time.Hour

type userKey struct{}

func currentUser(r *http.Request) (store.User, bool) {
	u, ok := r.Context().Value(userKey{}).(store.User)
	return u, ok
}

func (a *App) sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// guard enforces sign-in and the route's permission.
func (a *App) guard(perm string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if perm == "" {
			h(w, r)
			return
		}
		tok := a.sessionToken(r)
		var u store.User
		var err error = store.ErrNotFound
		if tok != "" {
			u, err = a.Store.SessionUser(r.Context(), auth.HashToken(tok), sessionTTL)
		}
		if err != nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeErr(w, 401, "Please sign in")
			} else {
				http.Redirect(w, r, "/#/signin", http.StatusSeeOther)
			}
			return
		}
		if u.MustChangePassword && r.URL.Path != "/api/me" && r.URL.Path != "/api/me/password" && r.URL.Path != "/api/state" {
			writeErr(w, 403, "Choose a new password before continuing")
			return
		}
		if !auth.Can(u.Role, perm) {
			writeErr(w, 403, fmt.Sprintf("Your role (%s) can't do this. Ask an admin if you need access.", auth.RoleName(u.Role)))
			return
		}
		ctx := context.WithValue(r.Context(), userKey{}, u)
		ctx = store.WithActor(ctx, store.Actor{ID: u.ID, Name: u.Name})
		h(w, r.WithContext(ctx))
	}
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
	return false
}

func (a *App) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Cross-Origin-Opener-Policy", "same-origin")
		if !strings.HasPrefix(r.URL.Path, "/demo-site/") {
			hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
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
			// Cross-site request protection: same origin plus a custom header a
			// cross-origin form cannot send without a CORS preflight.
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || !a.allowedHost(u.Host) || !strings.EqualFold(u.Host, r.Host) {
					http.Error(w, "Origin not allowed", http.StatusForbidden)
					return
				}
			}
			if r.Header.Get("X-NV-Request") != "1" {
				http.Error(w, "Missing request header", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		}
		sw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/jobs") && r.URL.Path != "/api/notifications" {
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

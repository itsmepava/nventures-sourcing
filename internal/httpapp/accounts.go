package httpapp

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/itsmepava/nventures-sourcing/internal/auth"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

// limiter slows password guessing: 5 failures per IP+email in 10 minutes
// lock that pair for 15 minutes.
type limiter struct {
	mu   sync.Mutex
	fail map[string][]time.Time
}

func (l *limiter) key(r *http.Request, email string) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip + "|" + strings.ToLower(strings.TrimSpace(email))
}

func (l *limiter) blocked(k string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	var recent []time.Time
	for _, t := range l.fail[k] {
		if time.Since(t) < 15*time.Minute {
			recent = append(recent, t)
		}
	}
	l.fail[k] = recent
	n := 0
	for _, t := range recent {
		if time.Since(t) < 10*time.Minute {
			n++
		}
	}
	return n >= 5
}

func (l *limiter) record(k string, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ok {
		delete(l.fail, k)
		return
	}
	l.fail[k] = append(l.fail[k], time.Now())
}

var logins = &limiter{fail: map[string][]time.Time{}}

func publicUser(u store.User) map[string]any {
	return map[string]any{"id": u.ID, "email": u.Email, "name": u.Name, "role": u.Role, "role_name": auth.RoleName(u.Role), "active": u.Active,
		"must_change_password": u.MustChangePassword, "created_at": u.CreatedAt, "last_login_at": u.LastLoginAt}
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, u store.User) error {
	tok := auth.RandomToken(32)
	if err := a.Store.CreateSession(r.Context(), auth.HashToken(tok), u.ID, sessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: strings.HasPrefix(a.Cfg.BaseURL, "https://"), MaxAge: int(sessionTTL.Seconds())})
	return nil
}

// authState tells the frontend whether to show setup, sign-in or the app.
func (a *App) authState(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"setup_needed": a.Store.CountUsers(r.Context()) == 0, "mode": a.Cfg.Mode, "version": a.Version}
	if tok := a.sessionToken(r); tok != "" {
		if u, err := a.Store.SessionUser(r.Context(), auth.HashToken(tok), sessionTTL); err == nil {
			out["user"] = publicUser(u)
		}
	}
	writeJSON(w, 200, out)
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(strings.TrimSpace(s))
	return err == nil && a.Address == strings.TrimSpace(s) && len(s) <= 254
}

func validName(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= 2 && n <= 100
}

// authSetup creates the first admin account. It only works while no
// account exists.
func (a *App) authSetup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Password string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	switch {
	case !validName(in.Name):
		writeErr(w, 400, "Enter your name (2–100 characters)")
		return
	case !validEmail(in.Email):
		writeErr(w, 400, "Enter a valid email address")
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, 500, "Could not secure the password")
		return
	}
	u, err := a.Store.CreateFirstAdmin(r.Context(), in.Email, in.Name, hash)
	if err != nil {
		writeErr(w, 409, "Setup is already complete; please sign in")
		return
	}
	ctx := store.WithActor(r.Context(), store.Actor{ID: u.ID, Name: u.Name})
	a.Store.Audit(ctx, 0, "user_created", fmt.Sprintf("Workspace set up; %s is the first admin", u.Name), map[string]any{"user_id": u.ID})
	if err := a.startSession(w, r, u); err != nil {
		writeErr(w, 500, "Could not sign in")
		return
	}
	writeJSON(w, 201, publicUser(u))
}

func (a *App) authLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	k := logins.key(r, in.Email)
	if logins.blocked(k) {
		writeErr(w, 429, "Too many failed attempts. Wait 15 minutes or ask an admin to reset your password.")
		return
	}
	u, err := a.Store.UserByEmail(r.Context(), in.Email)
	ok := err == nil && u.Active && auth.CheckPassword(u.PasswordHash(), in.Password)
	if err != nil {
		// Spend similar time for unknown accounts so timing does not reveal them.
		_ = auth.CheckPassword("pbkdf2-sha256$600000$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", in.Password)
	}
	logins.record(k, ok)
	if !ok {
		writeErr(w, 401, "Email or password is incorrect")
		return
	}
	if err := a.startSession(w, r, u); err != nil {
		writeErr(w, 500, "Could not sign in")
		return
	}
	a.Store.Audit(store.WithActor(r.Context(), store.Actor{ID: u.ID, Name: u.Name}), 0, "signed_in", u.Name+" signed in", nil)
	writeJSON(w, 200, publicUser(u))
}

func (a *App) authLogout(w http.ResponseWriter, r *http.Request) {
	if tok := a.sessionToken(r); tok != "" {
		a.Store.DeleteSession(r.Context(), auth.HashToken(tok))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	out := publicUser(u)
	out["permissions"] = auth.Permissions(u.Role)
	writeJSON(w, 200, out)
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	var in struct{ Current, New string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if !auth.CheckPassword(u.PasswordHash(), in.Current) {
		writeErr(w, 400, "Current password is incorrect")
		return
	}
	if err := auth.ValidatePassword(in.New); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.New == in.Current {
		writeErr(w, 400, "Choose a password different from the current one")
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		writeErr(w, 500, "Could not secure the password")
		return
	}
	if err := a.Store.SetPassword(r.Context(), u.ID, hash, false, auth.HashToken(a.sessionToken(r))); err != nil {
		writeErr(w, 500, "Could not save the password")
		return
	}
	a.Store.Audit(r.Context(), 0, "password_changed", u.Name+" changed their password (other sessions signed out)", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- team management (admin) ----

func (a *App) teamBrief(w http.ResponseWriter, r *http.Request) {
	us, err := a.Store.Users(r.Context())
	if err != nil {
		writeErr(w, 500, "Could not list the team")
		return
	}
	out := []map[string]any{}
	for _, u := range us {
		if u.Active {
			out = append(out, map[string]any{"id": u.ID, "name": u.Name, "role": u.Role, "role_name": auth.RoleName(u.Role)})
		}
	}
	writeJSON(w, 200, map[string]any{"members": out, "roles": auth.Roles})
}

func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	us, err := a.Store.Users(r.Context())
	if err != nil {
		writeErr(w, 500, "Could not list users")
		return
	}
	out := []map[string]any{}
	for _, u := range us {
		out = append(out, publicUser(u))
	}
	writeJSON(w, 200, map[string]any{"users": out, "roles": auth.Roles})
}

func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Role string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	switch {
	case !validName(in.Name):
		writeErr(w, 400, "Enter a name (2–100 characters)")
		return
	case !validEmail(in.Email):
		writeErr(w, 400, "Enter a valid email address")
		return
	case !auth.ValidRole(in.Role):
		writeErr(w, 400, "Choose a role")
		return
	}
	temp := auth.TempPassword()
	hash, err := auth.HashPassword(temp)
	if err != nil {
		writeErr(w, 500, "Could not create the account")
		return
	}
	u, err := a.Store.CreateUser(r.Context(), in.Email, in.Name, in.Role, hash, true)
	if errors.Is(err, store.ErrEmailTaken) {
		writeErr(w, 409, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 500, "Could not create the account")
		return
	}
	a.Store.Audit(r.Context(), 0, "user_created", fmt.Sprintf("%s added %s as %s", store.ActorFrom(r.Context()).Name, u.Name, auth.RoleName(u.Role)), map[string]any{"user_id": u.ID})
	out := publicUser(u)
	out["temporary_password"] = temp // shown once; the person must change it at first sign-in
	writeJSON(w, 201, out)
}

func (a *App) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	me, _ := currentUser(r)
	var in struct {
		Name   *string
		Role   *string
		Active *bool
	}
	if !ok || readJSON(r, &in) != nil {
		writeErr(w, 400, "Bad request")
		return
	}
	if in.Name != nil && !validName(*in.Name) {
		writeErr(w, 400, "Enter a name (2–100 characters)")
		return
	}
	if in.Role != nil && !auth.ValidRole(*in.Role) {
		writeErr(w, 400, "Unknown role")
		return
	}
	if id == me.ID && in.Active != nil && !*in.Active {
		writeErr(w, 400, "You can't deactivate your own account")
		return
	}
	u, err := a.Store.UpdateUser(r.Context(), id, in.Name, in.Role, in.Active)
	if errors.Is(err, store.ErrLastAdmin) {
		writeErr(w, 400, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 404, "User not found")
		return
	}
	a.Store.Audit(r.Context(), 0, "user_updated", fmt.Sprintf("%s updated %s (%s, %s)", me.Name, u.Name, auth.RoleName(u.Role), map[bool]string{true: "active", false: "deactivated"}[u.Active]), map[string]any{"user_id": u.ID})
	writeJSON(w, 200, publicUser(u))
}

func (a *App) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	u, err := a.Store.User(r.Context(), id)
	if !ok || err != nil {
		writeErr(w, 404, "User not found")
		return
	}
	temp := auth.TempPassword()
	hash, err := auth.HashPassword(temp)
	if err == nil {
		err = a.Store.SetPassword(r.Context(), id, hash, true, "")
	}
	if err != nil {
		writeErr(w, 500, "Could not reset the password")
		return
	}
	a.Store.Audit(r.Context(), 0, "password_reset", fmt.Sprintf("%s reset the password for %s", store.ActorFrom(r.Context()).Name, u.Name), map[string]any{"user_id": u.ID})
	writeJSON(w, 200, map[string]any{"temporary_password": temp})
}

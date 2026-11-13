package httpapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func jsonHdr() map[string]string {
	return map[string]string{"X-NV-Request": "1", "Content-Type": "application/json"}
}

func decode(t *testing.T, r *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	_ = json.NewDecoder(r.Body).Decode(&m)
	return m
}

// addMember creates an account as admin and completes the forced password change.
func addMember(t *testing.T, srv *httptest.Server, admin *http.Client, name, email, role string) *http.Client {
	t.Helper()
	r := do(t, admin, "POST", srv.URL+"/api/users", jsonHdr(), fmt.Sprintf(`{"name":%q,"email":%q,"role":%q}`, name, email, role))
	m := decode(t, r)
	if r.StatusCode != 201 {
		t.Fatalf("create %s: %d %v", role, r.StatusCode, m)
	}
	temp := m["temporary_password"].(string)
	c := signIn(t, srv, email, temp)
	if r := do(t, c, "GET", srv.URL+"/api/dashboard", nil, ""); r.StatusCode != 403 {
		t.Fatalf("temporary password allowed access before change: %d", r.StatusCode)
	}
	if r := do(t, c, "POST", srv.URL+"/api/me/password", jsonHdr(), fmt.Sprintf(`{"current":%q,"new":"a-new-strong-password"}`, temp)); r.StatusCode != 200 {
		t.Fatalf("password change %d", r.StatusCode)
	}
	return c
}

func TestSetupSignInSignOut(t *testing.T) {
	_, srv := newTestApp(t, nil)
	anon := &http.Client{}
	var st map[string]any
	st = decode(t, do(t, anon, "GET", srv.URL+"/api/auth/state", nil, ""))
	if st["setup_needed"] != true {
		t.Fatal("setup not needed on empty workspace")
	}
	if r := do(t, anon, "GET", srv.URL+"/api/state", nil, ""); r.StatusCode != 401 {
		t.Fatalf("API open without sign-in: %d", r.StatusCode)
	}
	if r := do(t, anon, "GET", srv.URL+"/", nil, ""); r.StatusCode != 200 || !strings.Contains(r.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("frontend shell not served: %d", r.StatusCode)
	}
	if r := do(t, anon, "POST", srv.URL+"/api/auth/setup", jsonHdr(), `{"name":"A","email":"x@y.z","password":"short"}`); r.StatusCode != 400 {
		t.Error("weak setup accepted")
	}
	admin := adminClient(t, srv)
	if r := do(t, anon, "POST", srv.URL+"/api/auth/setup", jsonHdr(), `{"name":"Second","email":"second@nventures-demo.test","password":"another-long-password"}`); r.StatusCode != 409 {
		t.Errorf("second setup allowed: %d", r.StatusCode)
	}
	me := decode(t, do(t, admin, "GET", srv.URL+"/api/me", nil, ""))
	if me["role"] != "admin" || len(me["permissions"].([]any)) < 10 {
		t.Fatalf("%v", me)
	}
	if r := do(t, anon, "POST", srv.URL+"/api/auth/login", jsonHdr(), `{"email":"admin@nventures-demo.test","password":"wrong-password"}`); r.StatusCode != 401 {
		t.Error("wrong password accepted")
	}
	if r := do(t, admin, "POST", srv.URL+"/api/auth/logout", jsonHdr(), ""); r.StatusCode != 200 {
		t.Fatal("logout")
	}
	if r := do(t, admin, "GET", srv.URL+"/api/state", nil, ""); r.StatusCode != 401 {
		t.Errorf("session still valid after logout: %d", r.StatusCode)
	}
}

func TestLoginLockout(t *testing.T) {
	_, srv := newTestApp(t, nil)
	adminClient(t, srv)
	anon := &http.Client{}
	codes := []int{}
	for i := 0; i < 6; i++ {
		codes = append(codes, do(t, anon, "POST", srv.URL+"/api/auth/login", jsonHdr(), `{"email":"lock@nventures-demo.test","password":"nope-nope-nope"}`).StatusCode)
	}
	if codes[4] != 401 || codes[5] != 429 {
		t.Fatalf("lockout codes %v", codes)
	}
}

func TestTeamManagementGuards(t *testing.T) {
	_, srv := newTestApp(t, nil)
	admin := adminClient(t, srv)
	me := decode(t, do(t, admin, "GET", srv.URL+"/api/me", nil, ""))
	id := int(me["id"].(float64))
	// The last admin can't be demoted or deactivated.
	if r := do(t, admin, "PATCH", fmt.Sprintf("%s/api/users/%d", srv.URL, id), jsonHdr(), `{"role":"viewer"}`); r.StatusCode != 400 {
		t.Errorf("last admin demoted: %d", r.StatusCode)
	}
	if r := do(t, admin, "PATCH", fmt.Sprintf("%s/api/users/%d", srv.URL, id), jsonHdr(), `{"active":false}`); r.StatusCode != 400 {
		t.Errorf("self deactivation: %d", r.StatusCode)
	}
	if r := do(t, admin, "POST", srv.URL+"/api/users", jsonHdr(), `{"name":"Dup","email":"ADMIN@nventures-demo.test","role":"viewer"}`); r.StatusCode != 409 {
		t.Errorf("duplicate email (case-insensitive) accepted: %d", r.StatusCode)
	}
	v := addMember(t, srv, admin, "Vee Viewer", "viewer@nventures-demo.test", "viewer")
	users := decode(t, do(t, admin, "GET", srv.URL+"/api/users", nil, ""))["users"].([]any)
	var vid int
	for _, u := range users {
		if u.(map[string]any)["email"] == "viewer@nventures-demo.test" {
			vid = int(u.(map[string]any)["id"].(float64))
		}
	}
	if r := do(t, admin, "PATCH", fmt.Sprintf("%s/api/users/%d", srv.URL, vid), jsonHdr(), `{"active":false}`); r.StatusCode != 200 {
		t.Fatal("deactivate")
	}
	if r := do(t, v, "GET", srv.URL+"/api/state", nil, ""); r.StatusCode != 401 {
		t.Errorf("deactivated member kept access: %d", r.StatusCode)
	}
	// Password reset forces a new temporary password and signs out sessions.
	do(t, admin, "PATCH", fmt.Sprintf("%s/api/users/%d", srv.URL, vid), jsonHdr(), `{"active":true}`)
	r := do(t, admin, "POST", fmt.Sprintf("%s/api/users/%d/reset-password", srv.URL, vid), jsonHdr(), "")
	if tp, _ := decode(t, r)["temporary_password"].(string); len(tp) != 19 {
		t.Errorf("reset: %q", tp)
	}
	evs := decode(t, do(t, admin, "GET", srv.URL+"/api/auth/state", nil, ""))
	if evs["user"] == nil {
		t.Error("admin session lost")
	}
}

// TestRoleMatrix checks every protected endpoint for every role: allowed
// requests never return 401/403, forbidden ones always return 403, and no
// request produces a server error.
func TestRoleMatrix(t *testing.T) {
	a, srv := newTestApp(t, nil)
	admin := adminClient(t, srv)
	// Seed some records so detail endpoints exist.
	do(t, admin, "POST", srv.URL+"/api/inbox/import", jsonHdr(), `{"limit":5}`)
	a.Jobs.Wait()
	clients := map[string]*http.Client{
		"admin":     admin,
		"sourcer":   addMember(t, srv, admin, "Sam Sourcer", "sourcer@nventures-demo.test", "sourcer"),
		"responder": addMember(t, srv, admin, "Rae Responder", "responder@nventures-demo.test", "responder"),
		"viewer":    addMember(t, srv, admin, "Vic Viewer", "viewer@nventures-demo.test", "viewer"),
	}
	type ep struct {
		method, path, body string
		allowed            []string
	}
	all := []string{"admin", "sourcer", "responder", "viewer"}
	eps := []ep{
		{"GET", "/api/state", "", all}, {"GET", "/api/dashboard", "", all}, {"GET", "/api/settings", "", all},
		{"GET", "/api/pitches", "", all}, {"GET", "/api/pitches/1", "", all}, {"GET", "/api/companies", "", all},
		{"GET", "/api/partners", "", all}, {"GET", "/api/jobs", "", all}, {"GET", "/api/events", "", all},
		{"GET", "/api/queue", "", all}, {"GET", "/api/notifications", "", all}, {"GET", "/api/team", "", all},
		{"GET", "/api/templates", "", all}, {"GET", "/api/candidates", "", all}, {"GET", "/api/notes?entity=pitch&id=1", "", all},
		{"GET", "/api/demo/sheet?id=demo-inbound-sheet&tab=Pitches", "", all}, {"GET", "/api/pitches/export.csv", "", all},
		{"GET", "/api/users", "", []string{"admin"}}, {"GET", "/api/schedules", "", []string{"admin"}},
		{"POST", "/api/settings", `{"llm_enabled":false}`, []string{"admin"}},
		{"POST", "/api/pitches/1", `{"name":"Kelp Ledger (demo)","description":"A long enough description here","contact":"a@b.co","decision":"review"}`, []string{"admin", "responder"}},
		{"POST", "/api/pitches/1/workflow", `{"stage":"reviewing"}`, []string{"admin", "responder"}},
		{"POST", "/api/pitches/1/decks", `{"filename":"not-attached.pdf"}`, []string{"admin", "responder"}},
		{"POST", "/api/pitches/1/star", `{"on":true}`, []string{"admin", "sourcer", "responder"}},
		{"POST", "/api/notes", `{"entity":"pitch","id":1,"body":"hello"}`, []string{"admin", "responder"}},
		{"POST", "/api/partners", `{"name":"Matrix Fund %s","website":"https://m.example"}`, []string{"admin", "sourcer"}},
		{"POST", "/api/templates", `{"name":"Tmpl %s","subject":"S","body":"B"}`, []string{"admin"}},
		{"POST", "/api/schedules", `{"kind":"outbound_run","every_minutes":1440,"enabled":false}`, []string{"admin"}},
		{"POST", "/api/users", `{"name":"New %s","email":"new-%s@nventures-demo.test","role":"viewer"}`, []string{"admin"}},
		{"POST", "/api/cache/clear", ``, []string{"admin"}},
		{"POST", "/api/google/test", ``, []string{"admin"}},
	}
	for _, e := range eps {
		for role, c := range clients {
			body := strings.ReplaceAll(e.body, "%s", role)
			r := do(t, c, e.method, srv.URL+e.path, jsonHdr(), body)
			allowed := false
			for _, x := range e.allowed {
				if x == role {
					allowed = true
				}
			}
			switch {
			case r.StatusCode >= 500:
				t.Errorf("%s %s as %s: server error %d", e.method, e.path, role, r.StatusCode)
			case allowed && (r.StatusCode == 401 || r.StatusCode == 403):
				t.Errorf("%s %s as %s: refused (%d)", e.method, e.path, role, r.StatusCode)
			case !allowed && r.StatusCode != 403:
				t.Errorf("%s %s as %s: expected 403, got %d", e.method, e.path, role, r.StatusCode)
			}
		}
	}
	// Jobs: a sourcer can't start or retry inbox jobs, a responder can't run sourcing.
	if r := do(t, clients["sourcer"], "POST", srv.URL+"/api/inbox/import", jsonHdr(), `{"limit":1}`); r.StatusCode != 403 {
		t.Errorf("sourcer imported: %d", r.StatusCode)
	}
	if r := do(t, clients["responder"], "POST", srv.URL+"/api/sourcing/run", jsonHdr(), `{"target":1}`); r.StatusCode != 403 {
		t.Errorf("responder ran sourcing: %d", r.StatusCode)
	}
	js, _ := a.Store.Jobs(t.Context(), "inbound_import", 1)
	if r := do(t, clients["sourcer"], "POST", fmt.Sprintf("%s/api/jobs/%d/cancel", srv.URL, js[0].ID), jsonHdr(), ""); r.StatusCode != 403 {
		t.Errorf("sourcer cancelled an inbox job: %d", r.StatusCode)
	}
}

func TestAssignmentNotificationQueueAndAudit(t *testing.T) {
	a, srv := newTestApp(t, nil)
	admin := adminClient(t, srv)
	do(t, admin, "POST", srv.URL+"/api/inbox/import", jsonHdr(), `{"limit":5}`)
	a.Jobs.Wait()
	resp := addMember(t, srv, admin, "Rae Responder", "responder@nventures-demo.test", "responder")
	team := decode(t, do(t, admin, "GET", srv.URL+"/api/team", nil, ""))["members"].([]any)
	var rid float64
	for _, m := range team {
		if m.(map[string]any)["name"] == "Rae Responder" {
			rid = m.(map[string]any)["id"].(float64)
		}
	}
	due := time.Now().Format("2006-01-02")
	if r := do(t, admin, "POST", srv.URL+"/api/pitches/1/workflow", jsonHdr(), fmt.Sprintf(`{"assignee_id":%v,"stage":"contacted","follow_up_at":%q}`, rid, due)); r.StatusCode != 200 {
		t.Fatalf("assign: %d", r.StatusCode)
	}
	if r := do(t, admin, "POST", srv.URL+"/api/pitches/1/workflow", jsonHdr(), `{"stage":"bogus"}`); r.StatusCode != 400 {
		t.Error("bad stage accepted")
	}
	n := decode(t, do(t, resp, "GET", srv.URL+"/api/notifications", nil, ""))
	if n["unread"].(float64) < 1 || !strings.Contains(fmt.Sprint(n["items"]), "assigned you") {
		t.Fatalf("no assignment notification: %v", n)
	}
	q := decode(t, do(t, resp, "GET", srv.URL+"/api/queue", nil, ""))
	if len(q["items"].([]any)) != 1 || q["due"].(float64) != 1 {
		t.Fatalf("queue %v", q)
	}
	do(t, resp, "POST", srv.URL+"/api/notifications/read", jsonHdr(), `{}`)
	if n := decode(t, do(t, resp, "GET", srv.URL+"/api/notifications", nil, "")); n["unread"].(float64) != 0 {
		t.Error("mark read")
	}
	do(t, resp, "POST", srv.URL+"/api/notes", jsonHdr(), `{"entity":"pitch","id":1,"body":"Called the founder"}`)
	var notes []map[string]any
	json.NewDecoder(do(t, admin, "GET", srv.URL+"/api/notes?entity=pitch&id=1", nil, "").Body).Decode(&notes)
	if len(notes) != 1 || notes[0]["author"] != "Rae Responder" {
		t.Fatalf("notes %v", notes)
	}
	var evs []map[string]any
	json.NewDecoder(do(t, admin, "GET", srv.URL+"/api/events?kind=workflow", nil, "").Body).Decode(&evs)
	if len(evs) == 0 || evs[0]["actor"] != "Test Admin" {
		t.Fatalf("audit actor missing: %v", evs)
	}
	var jobs []map[string]any
	json.NewDecoder(do(t, admin, "GET", srv.URL+"/api/jobs", nil, "").Body).Decode(&jobs)
	if jobs[0]["created_by_name"] != "Test Admin" {
		t.Errorf("job creator missing: %v", jobs[0]["created_by_name"])
	}
}

func TestSchedulerRunsDueScheduleAndNotifies(t *testing.T) {
	a, srv := newTestApp(t, nil)
	admin := adminClient(t, srv)
	sourcer := addMember(t, srv, admin, "Sam Sourcer", "sourcer@nventures-demo.test", "sourcer")
	r := do(t, admin, "POST", srv.URL+"/api/schedules", jsonHdr(), `{"kind":"outbound_run","every_minutes":60,"enabled":true,"target":3,"max_partners":2,"sync":true}`)
	x := decode(t, r)
	if r.StatusCode != 200 {
		t.Fatalf("schedule %v", x)
	}
	if r := do(t, admin, "POST", srv.URL+"/api/schedules", jsonHdr(), `{"kind":"outbound_run","every_minutes":5}`); r.StatusCode != 400 {
		t.Error("too-frequent schedule accepted")
	}
	// Make it due and run the scheduler loop briefly.
	if _, err := a.Store.DB.Exec(`UPDATE schedules SET next_run_at='2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	a.schedulerTick = 20 * time.Millisecond
	stop := make(chan struct{})
	go a.scheduler(stop)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if xs, _ := a.Store.Schedules(t.Context()); len(xs) == 1 && xs[0].LastJobID != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	for time.Now().Before(deadline) {
		if js, _ := a.Store.Jobs(t.Context(), "outbound_run", 1); len(js) == 1 && js[0].Status != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.Jobs.Wait()
	js, _ := a.Store.Jobs(t.Context(), "outbound_run", 5)
	if len(js) != 1 || js[0].CreatedByName != "Scheduler" {
		t.Fatalf("scheduled job: %+v", js)
	}
	xs, _ := a.Store.Schedules(t.Context())
	if xs[0].LastJobID == nil || xs[0].NextRunAt < time.Now().UTC().Format(time.RFC3339) {
		t.Errorf("schedule not advanced: %+v", xs[0])
	}
	n := decode(t, do(t, sourcer, "GET", srv.URL+"/api/notifications", nil, ""))
	if !strings.Contains(fmt.Sprint(n["items"]), "Sourcing run") {
		t.Errorf("sourcer not notified of scheduled run: %v", n)
	}
	_ = cookiejar.New
}

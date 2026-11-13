package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	Iterations = 1000
	h, err := HashPassword("correct horse battery")
	if err != nil || !strings.HasPrefix(h, "pbkdf2-sha256$1000$") {
		t.Fatal(h, err)
	}
	if !CheckPassword(h, "correct horse battery") || CheckPassword(h, "wrong") || CheckPassword("garbage", "x") {
		t.Fatal("check failed")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Error("salt not random")
	}
	if ValidatePassword("short") == nil || ValidatePassword("long enough pw") != nil {
		t.Error("policy")
	}
	if tp := TempPassword(); len(tp) != 19 || ValidatePassword(tp) != nil {
		t.Errorf("temp %q", tp)
	}
}

func TestRoleMatrix(t *testing.T) {
	cases := []struct {
		role, perm string
		want       bool
	}{
		{Admin, UsersManage, true}, {Sourcer, UsersManage, false}, {Responder, SettingsManage, false},
		{Sourcer, SourcingRun, true}, {Sourcer, InboxImport, false}, {Sourcer, PitchWork, false},
		{Responder, InboxImport, true}, {Responder, SourcingRun, false}, {Responder, CompanyWork, false},
		{Viewer, View, true}, {Viewer, Shortlist, false}, {Viewer, InboxReview, false},
		{"unknown", View, false},
	}
	for _, c := range cases {
		if Can(c.role, c.perm) != c.want {
			t.Errorf("%s %s", c.role, c.perm)
		}
	}
	if JobPermission("outbound_research") != SourcingRun || JobPermission("x") != SettingsManage {
		t.Error("job perms")
	}
}

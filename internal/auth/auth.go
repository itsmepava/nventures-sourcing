// Package auth holds password hashing and the role/permission model. It uses
// only the Go standard library (crypto/pbkdf2, crypto/rand, crypto/subtle).
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Iterations is the PBKDF2-SHA256 work factor (OWASP 2023+ guidance).
// Tests lower it to keep the suite fast.
var Iterations = 600_000

// MinPasswordLen is the minimum accepted password length.
const MinPasswordLen = 10

// HashPassword returns "pbkdf2-sha256$<iter>$<salt>$<hash>".
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, Iterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", Iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// CheckPassword verifies pw against a stored hash in constant time.
func CheckPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// ValidatePassword enforces the password policy.
func ValidatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLen {
		return fmt.Errorf("Use at least %d characters", MinPasswordLen)
	}
	if utf8.RuneCountInString(pw) > 200 {
		return errors.New("Password is too long")
	}
	return nil
}

// RandomToken returns n random bytes, URL-safe encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// TempPassword returns a readable one-time password.
func TempPassword() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	out := make([]byte, 16)
	for i, c := range b {
		out[i] = alphabet[int(c)%len(alphabet)]
	}
	return string(out[:4]) + "-" + string(out[4:8]) + "-" + string(out[8:12]) + "-" + string(out[12:])
}

// HashToken stores session tokens as SHA-256 so a database copy cannot be
// used to hijack sessions.
func HashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// Roles.
const (
	Admin     = "admin"
	Sourcer   = "sourcer"
	Responder = "responder"
	Viewer    = "viewer"
)

// Roles lists roles with their display names, most privileged first.
var Roles = []struct{ ID, Name, Summary string }{
	{Admin, "Admin", "Everything, including team members, settings, schedules and the Google connection"},
	{Sourcer, "Sourcer", "Runs outbound sourcing, manages partners and works sourced companies"},
	{Responder, "Pitch responder", "Imports, reviews and syncs inbound pitches and follows up with founders"},
	{Viewer, "Viewer", "Read-only access to every workspace"},
}

// ValidRole reports whether r is a known role.
func ValidRole(r string) bool {
	for _, x := range Roles {
		if x.ID == r {
			return true
		}
	}
	return false
}

// RoleName returns the display name of a role.
func RoleName(r string) string {
	for _, x := range Roles {
		if x.ID == r {
			return x.Name
		}
	}
	return r
}

// Permissions.
const (
	View            = "view"
	InboxImport     = "inbox.import"
	InboxReview     = "inbox.review"
	InboxSync       = "inbox.sync"
	PitchWork       = "pitch.work" // assign, stage, follow-up, notes
	SourcingRun     = "sourcing.run"
	SourcingSync    = "sourcing.sync"
	PartnersEdit    = "partners.edit"
	CompanyWork     = "company.work" // assign, stage, follow-up, notes
	Shortlist       = "shortlist"
	JobsControl     = "jobs.control" // cancel/retry jobs of the roles' own workflows
	TemplatesUse    = "templates.use"
	TemplatesManage = "templates.manage"
	SettingsManage  = "settings.manage"
	UsersManage     = "users.manage"
	SchedulesManage = "schedules.manage"
	DemoReset       = "demo.reset"
)

var matrix = map[string][]string{
	Admin:     {View, InboxImport, InboxReview, InboxSync, PitchWork, SourcingRun, SourcingSync, PartnersEdit, CompanyWork, Shortlist, JobsControl, TemplatesUse, TemplatesManage, SettingsManage, UsersManage, SchedulesManage, DemoReset},
	Sourcer:   {View, SourcingRun, SourcingSync, PartnersEdit, CompanyWork, Shortlist, JobsControl},
	Responder: {View, InboxImport, InboxReview, InboxSync, PitchWork, Shortlist, JobsControl, TemplatesUse},
	Viewer:    {View},
}

// Can reports whether role has permission perm.
func Can(role, perm string) bool {
	for _, p := range matrix[role] {
		if p == perm {
			return true
		}
	}
	return false
}

// Permissions returns the permissions of a role (for the frontend).
func Permissions(role string) []string {
	return append([]string{}, matrix[role]...)
}

// JobPermission maps a job kind to the permission needed to start/cancel it.
func JobPermission(kind string) string {
	switch kind {
	case "inbound_import":
		return InboxImport
	case "inbound_sync":
		return InboxSync
	case "outbound_run", "outbound_research":
		return SourcingRun
	case "outbound_sync":
		return SourcingSync
	}
	return SettingsManage
}

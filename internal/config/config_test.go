package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	m, err := ParseDotenv("# c\n\nexport A=1\nB = two words # note\nC=\"x\\ny # kept\"\nD='lit\\n'\nE=\nF=a#b\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "two words", "C": "x\ny # kept", "D": `lit\n`, "E": "", "F": "a#b"}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s=%q want %q", k, m[k], v)
		}
	}
	for _, bad := range []string{"NOEQUALS", "1A=x", `A="unterminated`} {
		if _, err := ParseDotenv(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestLoadLegacyNamesAndBindSafety(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".env")
	os.WriteFile(f, []byte("APP_MODE=live\nINBOX_SPREADSHEET_ID=sheet-x\nSPREADSHEET_ID=out-y\nTAVILY_API_KEY=k\nGMAIL_EXPECTED_ACCOUNT=Pitches@Example.com\n"), 0o600)
	t.Setenv("NV_DATA_DIR", dir)
	c, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != "live" || c.InboundSheet != "sheet-x" || c.OutboundSheet != "out-y" || !c.TavilyKeyPresent || c.ExpectedAccount != "pitches@example.com" || c.Port != 8765 || !c.Loopback() {
		t.Fatalf("%+v", c)
	}
	if c.DBPath() == filepath.Join(dir, "demo", "nventures-demo.db") {
		t.Error("live used demo db")
	}
	t.Setenv("APP_MODE", "demo")
	c, _ = Load(f)
	if c.Mode != "demo" {
		t.Error("process env should win")
	}
	t.Setenv("NV_HOST", "0.0.0.0")
	if _, err := Load(f); err == nil {
		t.Error("public bind without explicit opt-in accepted")
	}
	t.Setenv("NV_ALLOW_REMOTE", "1")
	if _, err := Load(f); err == nil {
		t.Error("public bind without base URL accepted")
	}
	t.Setenv("NV_BASE_URL", "https://sourcing.example")
	if _, err := Load(f); err != nil {
		t.Error(err)
	}
}

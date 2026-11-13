// Package config reads configuration from environment variables and an
// optional .env file. It accepts the legacy Python app's variable names so
// an existing .env keeps working unchanged.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Config is the process configuration.
type Config struct {
	Mode        string // demo | live
	DataDir     string
	Host        string
	Port        int
	BaseURL     string // external URL when behind a proxy (future hosting)
	AllowRemote bool   // explicit opt-in for a non-loopback bind (accounts still required)
	NoBrowser   bool
	EnvFile     string
	LogJSON     bool

	// Google (legacy names accepted).
	OAuthClientID     string
	OAuthClientSecret string
	OAuthRefreshToken string
	ServiceAccount    string
	ExpectedAccount   string
	GmailQuery        string
	InboundSheet      string
	InboundTab        string
	OutboundSheet     string
	OutboundTab       string
	PartnerTab        string
	ControlTab        string
	OutboundSync      bool
	MaxResearch       int

	// Optional local inference (opt-in in Settings as well).
	OllamaURL string

	// Legacy paid adapters: detected for honest reporting only. The Go app
	// does not call them.
	TavilyKeyPresent     bool
	OpenRouterKeyPresent bool
}

// ParseDotenv parses the supported .env syntax:
//
//	KEY=value            # comment after whitespace (unquoted only)
//	export KEY=value
//	KEY="double quoted with \n escapes"
//	KEY='single quoted, literal'
//
// Blank lines and lines starting with # are ignored. Keys must match
// [A-Za-z_][A-Za-z0-9_]*. Values never undergo variable expansion.
func ParseDotenv(data string) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\uFEFF"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf(".env line %d: expected KEY=value", n)
		}
		k = strings.TrimSpace(k)
		if !validKey(k) {
			return nil, fmt.Errorf(".env line %d: invalid key", n)
		}
		v = strings.TrimSpace(v)
		switch {
		case strings.HasPrefix(v, `"`):
			end := strings.LastIndex(v, `"`)
			if end <= 0 {
				return nil, fmt.Errorf(".env line %d: unterminated quote", n)
			}
			v = strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`).Replace(v[1:end])
		case strings.HasPrefix(v, `'`):
			end := strings.LastIndex(v, `'`)
			if end <= 0 {
				return nil, fmt.Errorf(".env line %d: unterminated quote", n)
			}
			v = v[1:end]
		default:
			if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
		}
		out[k] = v
	}
	return out, sc.Err()
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// DefaultDataDir returns the per-user data directory.
func DefaultDataDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "nVentures")
		}
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "nventures")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "nventures-data"
	}
	return filepath.Join(home, ".local", "share", "nventures")
}

// Load reads env and the optional .env file. Process environment wins.
func Load(envFile string) (Config, error) {
	file := map[string]string{}
	if envFile != "" {
		b, err := os.ReadFile(envFile)
		if err == nil {
			if file, err = ParseDotenv(string(b)); err != nil {
				return Config{}, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
	}
	get := func(def string, keys ...string) string {
		for _, k := range keys {
			if v, ok := os.LookupEnv(k); ok && v != "" {
				return v
			}
			if v := file[k]; v != "" {
				return v
			}
		}
		return def
	}
	c := Config{EnvFile: envFile}
	c.Mode = strings.ToLower(get("demo", "NV_MODE", "APP_MODE"))
	if c.Mode != "demo" && c.Mode != "live" {
		return c, errors.New("APP_MODE must be demo or live")
	}
	c.DataDir = get(DefaultDataDir(), "NV_DATA_DIR")
	c.Host = get("127.0.0.1", "NV_HOST")
	port, err := strconv.Atoi(get("8765", "NV_PORT", "PORT"))
	if err != nil || port < 0 || port > 65535 {
		return c, errors.New("NV_PORT must be a port number")
	}
	c.Port = port
	c.BaseURL = strings.TrimRight(get("", "NV_BASE_URL"), "/")
	c.AllowRemote = get("", "NV_ALLOW_REMOTE") == "1"
	c.NoBrowser = get("", "NV_NO_BROWSER") == "1" || strings.EqualFold(get("", "NV_NO_BROWSER"), "true")
	c.LogJSON = get("", "NV_LOG_JSON") == "1"
	c.OAuthClientID = get("", "GOOGLE_OAUTH_CLIENT_ID")
	c.OAuthClientSecret = get("", "GOOGLE_OAUTH_CLIENT_SECRET")
	c.OAuthRefreshToken = get("", "GOOGLE_OAUTH_REFRESH_TOKEN")
	c.ServiceAccount = get("", "GOOGLE_SERVICE_ACCOUNT_JSON")
	c.ExpectedAccount = strings.ToLower(get("", "GMAIL_EXPECTED_ACCOUNT"))
	c.GmailQuery = get("", "GMAIL_PITCH_QUERY")
	c.InboundSheet = get("", "INBOX_SPREADSHEET_ID")
	c.InboundTab = get("Sheet1", "INBOX_SHEET_TAB")
	c.OutboundSheet = get("", "SPREADSHEET_ID")
	c.OutboundTab = get("Active Sourcing", "SOURCING_TAB_NAME")
	c.PartnerTab = get("Partner List", "PARTNER_TAB_NAME")
	c.ControlTab = get("Sourcing Control", "CONTROL_TAB_NAME")
	c.OutboundSync = strings.EqualFold(get("false", "SHEETS_SYNC_ENABLED"), "true")
	c.MaxResearch, _ = strconv.Atoi(get("45", "MAX_RESEARCH_PER_RUN"))
	if c.MaxResearch <= 0 || c.MaxResearch > 500 {
		c.MaxResearch = 45
	}
	c.OllamaURL = get("http://127.0.0.1:11434", "NV_OLLAMA_URL")
	c.TavilyKeyPresent = get("", "TAVILY_API_KEY") != ""
	c.OpenRouterKeyPresent = get("", "OPENROUTER_API_KEY") != ""
	if !c.Loopback() && (!c.AllowRemote || c.BaseURL == "") {
		return c, errors.New("binding to a non-loopback address requires NV_ALLOW_REMOTE=1 and NV_BASE_URL; the default is 127.0.0.1 (this computer only)")
	}
	return c, nil
}

// Loopback reports whether the bind host is local only.
func (c Config) Loopback() bool {
	if c.Host == "localhost" {
		return true
	}
	ip := net.ParseIP(c.Host)
	return ip != nil && ip.IsLoopback()
}

// DBPath returns the mode-specific database path. Demo and live data are
// stored separately and never mixed.
func (c Config) DBPath() string {
	if c.Mode == "demo" {
		return filepath.Join(c.DataDir, "demo", "nventures-demo.db")
	}
	return filepath.Join(c.DataDir, "live", "nventures.db")
}

// ModeDir returns the per-mode data directory.
func (c Config) ModeDir() string { return filepath.Dir(c.DBPath()) }

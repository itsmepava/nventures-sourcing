// Command nventures is the all-in-one nVentures Sourcing application: one
// executable serving the embedded UI, API, migrations and background jobs
// on localhost, backed by a local SQLite database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/config"
	"github.com/itsmepava/nventures-sourcing/internal/httpapp"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.4.0-alpha.1-dev"

func main() {
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "rehearse":
		err = rehearse(args)
	case "migrate-python":
		err = migratePython(args)
	case "backup":
		err = backup(args)
	case "version":
		fmt.Println("nventures", version, runtime.GOOS+"/"+runtime.GOARCH, runtime.Version())
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "nventures:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`nVentures Sourcing ` + version + `

Usage:
  nventures [serve] [--env FILE] [--no-browser]   start the local app (default)
  nventures rehearse [--keep DIR]                 run the deterministic demo rehearsal
  nventures migrate-python --from DB [--mode demo|live]
                                                  import a Python app database (read-only source)
  nventures backup [--out FILE]                   consistent SQLite backup of the current mode
  nventures version

Configuration comes from environment variables and an optional .env file
(APP_MODE, NV_DATA_DIR, NV_PORT, Google settings). See docs/GO-APP.md.
`)
}

func defaultEnvFile() string {
	if _, err := os.Stat(".env"); err == nil {
		return ".env"
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), ".env")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func loadConfig(fs *flag.FlagSet, args []string) (config.Config, error) {
	env := fs.String("env", defaultEnvFile(), "path to .env file (optional)")
	if err := fs.Parse(args); err != nil {
		return config.Config{}, err
	}
	return config.Load(*env)
}

func newLogger(cfg config.Config) (*slog.Logger, io.Closer) {
	var w io.Writer = os.Stderr
	var closer io.Closer = io.NopCloser(nil)
	if err := os.MkdirAll(cfg.ModeDir(), 0o700); err == nil {
		path := filepath.Join(cfg.ModeDir(), "nventures.log")
		if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
			_ = os.Rename(path, path+".1")
		}
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			w = io.MultiWriter(os.Stderr, f)
			closer = f
		}
	}
	var h slog.Handler = slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
	if cfg.LogJSON {
		h = slog.NewJSONHandler(w, nil)
	}
	return slog.New(h), closer
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	noBrowser := fs.Bool("no-browser", false, "do not open a browser window")
	cfg, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	if *noBrowser {
		cfg.NoBrowser = true
	}
	log, closer := newLogger(cfg)
	defer closer.Close()
	app, err := httpapp.New(cfg, version, log)
	if err != nil {
		return err
	}
	defer app.Close()
	ln, err := listen(cfg)
	if err != nil {
		return err
	}
	app.Port = ln.Addr().(*net.TCPAddr).Port
	app.StartScheduler()
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 120 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/", app.Port)
	if !cfg.Loopback() {
		url = fmt.Sprintf("http://%s/", ln.Addr())
	}
	log.Info("nVentures started", "version", version, "mode", cfg.Mode, "url", url, "data", cfg.ModeDir())
	fmt.Printf("\n  nVentures Sourcing %s (%s mode)\n  Open %s\n  Press Ctrl+C to stop.\n\n", version, cfg.Mode, url)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	if !cfg.NoBrowser {
		go openBrowser(url)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	return nil
}

func listen(cfg config.Config) (net.Listener, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err == nil || cfg.Port == 0 || os.Getenv("NV_PORT") != "" {
		if err != nil {
			return nil, fmt.Errorf("port %d is in use; set NV_PORT to another port: %w", cfg.Port, err)
		}
		return ln, nil
	}
	for p := cfg.Port + 1; p <= cfg.Port+10; p++ {
		if ln, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(p))); err == nil {
			return ln, nil
		}
	}
	return nil, fmt.Errorf("ports %d-%d are in use; set NV_PORT", cfg.Port, cfg.Port+10)
}

func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var name string
	var args []string
	switch runtime.GOOS {
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		name, args = "open", []string{url}
	default:
		name, args = "xdg-open", []string{url}
	}
	if p, err := exec.LookPath(name); err == nil {
		_ = exec.Command(p, args...).Start()
	}
}

func rehearse(args []string) error {
	fs := flag.NewFlagSet("rehearse", flag.ExitOnError)
	keep := fs.String("keep", "", "keep rehearsal data in this directory (default: temporary)")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	_ = fs.Parse(args)
	dir := *keep
	if dir == "" {
		tmp, err := os.MkdirTemp("", "nventures-rehearsal-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		dir = tmp
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rep, err := httpapp.Rehearse(dir, version, log)
	if err != nil {
		return err
	}
	if *asJSON {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println("nVentures deterministic rehearsal (demo fixtures, no external credentials)")
		for _, s := range rep.Steps {
			fmt.Println("  ✓", s)
		}
		fmt.Printf("  Inbound sheet rows: %d · Outbound sheet rows: %d · Pages fetched: %d · Cache hits: %d\n", rep.InboundRows, rep.OutboundRows, rep.Crawler["pages_fetched"], rep.Crawler["cache_hits"])
		for _, f := range rep.Failures {
			fmt.Println("  ✗", f)
		}
	}
	if !rep.Passed {
		return errors.New("rehearsal failed")
	}
	fmt.Println("PASS")
	return nil
}

func migratePython(args []string) error {
	fs := flag.NewFlagSet("migrate-python", flag.ExitOnError)
	from := fs.String("from", "", "path to the Python app SQLite database")
	mode := fs.String("mode", "", "target mode (demo|live); defaults to APP_MODE")
	report := fs.String("report", "", "write the JSON report here")
	cfg, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	if *from == "" {
		return errors.New("--from is required")
	}
	if *mode != "" {
		cfg.Mode = *mode
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	rep, err := st.MigratePython(context.Background(), *from, cfg.Mode, filepath.Join(cfg.ModeDir(), "backups"))
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	if *report != "" {
		if err := os.WriteFile(*report, b, 0o600); err != nil {
			return err
		}
	}
	fmt.Println(string(b))
	if !rep.SourceUnchanged {
		return errors.New("source database changed during import; investigate before relying on it")
	}
	return nil
}

func backup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	out := fs.String("out", "", "backup file path (default: data dir/backups/...)")
	cfg, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	dest := *out
	if dest == "" {
		dest = filepath.Join(cfg.ModeDir(), "backups", "nventures-"+cfg.Mode+"-"+time.Now().UTC().Format("20060102-150405")+".db")
	}
	if err := st.Backup(context.Background(), dest); err != nil {
		return err
	}
	fmt.Println("Backup written:", dest)
	return nil
}

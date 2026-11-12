// Package jobs runs durable background work: inbound Gmail import, outbound
// sourcing, and verified sheet synchronisation. Job state, progress, events
// and leases live in SQLite so restarts produce honest interrupted outcomes.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/discovery"
	"github.com/itsmepava/nventures-sourcing/internal/google"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

// Conn is an authorised Google connection.
type Conn struct {
	Gmail   *google.Gmail
	Sheets  *google.Sheets
	Account string // mailbox verified via Gmail profile, if any
}

// Config is the effective runtime configuration for a job.
type Config struct {
	Mode            string
	ExpectedAccount string
	GmailQuery      string
	InboundSheet    string
	InboundTab      string
	OutboundSheet   string
	OutboundTab     string
	PartnerTab      string
	ControlTab      string
	MaxResearch     int
}

// Env provides dependencies.
type Env struct {
	Store   *store.Store
	Crawler *discovery.Crawler
	Config  func(ctx context.Context) Config
	Connect func(ctx context.Context) (*Conn, error)
	Owner   string
	Log     *slog.Logger
}

// Kinds and the lease each one holds. One lease per workflow guarantees a
// single writer per destination sheet.
var leaseFor = map[string]string{
	"inbound_import": "job:inbound", "inbound_sync": "job:inbound",
	"outbound_run": "job:outbound", "outbound_sync": "job:outbound", "outbound_research": "job:outbound",
}

// ErrBusy is returned when the workflow already has a running job.
var ErrBusy = errors.New("busy")

// Manager starts and tracks jobs.
type Manager struct {
	env     Env
	mu      sync.Mutex
	cancels map[int64]context.CancelFunc
	wg      sync.WaitGroup
	base    context.Context
	stop    context.CancelFunc
	// TTL for job leases; renewed by heartbeat.
	TTL time.Duration
}

// NewManager creates a manager.
func NewManager(env Env) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	if env.Log == nil {
		env.Log = slog.Default()
	}
	return &Manager{env: env, cancels: map[int64]context.CancelFunc{}, base: ctx, stop: cancel, TTL: 2 * time.Minute}
}

// Running returns the running job for a workflow lease, if any.
func (m *Manager) Running(ctx context.Context, kind string) (int64, bool) {
	return m.env.Store.LeaseHolder(ctx, leaseFor[kind])
}

// Start creates a job and runs it in the background.
func (m *Manager) Start(ctx context.Context, kind string, params map[string]any) (store.Job, error) {
	lease, ok := leaseFor[kind]
	if !ok {
		return store.Job{}, fmt.Errorf("unknown job kind %q", kind)
	}
	cfg := m.env.Config(ctx)
	// Take the lease before creating the job so concurrent starts cannot race.
	owner := m.env.Owner + ":" + kind + ":" + fmt.Sprint(time.Now().UnixNano())
	got, err := m.env.Store.AcquireLease(ctx, lease, owner, 0, m.TTL)
	if err != nil {
		return store.Job{}, err
	}
	if !got {
		return store.Job{}, ErrBusy
	}
	job, err := m.env.Store.CreateJob(ctx, kind, cfg.Mode, params)
	if err != nil {
		_ = m.env.Store.ReleaseLease(ctx, lease, owner)
		return store.Job{}, err
	}
	_, _ = m.env.Store.AcquireLease(ctx, lease, owner, job.ID, m.TTL)
	jctx, cancel := context.WithCancel(m.base)
	m.mu.Lock()
	m.cancels[job.ID] = cancel
	m.mu.Unlock()
	m.env.Store.Audit(ctx, job.ID, "started", fmt.Sprintf("%s started (%s mode)", label(kind), cfg.Mode), params)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			delete(m.cancels, job.ID)
			m.mu.Unlock()
			cancel()
			_ = m.env.Store.ReleaseLease(context.Background(), lease, owner)
		}()
		hb := time.NewTicker(m.TTL / 4)
		defer hb.Stop()
		go func() {
			for {
				select {
				case <-jctx.Done():
					return
				case <-hb.C:
					if err := m.env.Store.RenewLease(context.Background(), lease, owner, m.TTL); err != nil {
						m.env.Log.Warn("lease renewal failed", "job", job.ID, "err", err)
					}
					if m.env.Store.CancelRequested(jctx, job.ID) {
						cancel()
					}
				}
			}
		}()
		r := &run{m: m, env: m.env, job: job, cfg: cfg, ctx: jctx, cancel: cancel, counters: map[string]any{}}
		r.execute(kind, params)
	}()
	return job, nil
}

// Cancel requests cancellation of a job.
func (m *Manager) Cancel(ctx context.Context, id int64) error {
	if err := m.env.Store.RequestCancel(ctx, id); err != nil {
		return err
	}
	m.mu.Lock()
	c := m.cancels[id]
	m.mu.Unlock()
	if c != nil {
		c()
	}
	m.env.Store.Audit(ctx, id, "cancel", "Cancellation requested; in-flight network work stops at the next safe point", nil)
	return nil
}

// Wait blocks until all jobs end (tests and shutdown).
func (m *Manager) Wait() { m.wg.Wait() }

// Shutdown cancels running jobs and waits up to the context deadline.
func (m *Manager) Shutdown(ctx context.Context) {
	m.stop()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func label(kind string) string {
	return map[string]string{"inbound_import": "Inbox import", "inbound_sync": "Inbound sheet sync", "outbound_run": "Sourcing run", "outbound_sync": "Outbound sheet sync", "outbound_research": "Company research"}[kind]
}

type run struct {
	m        *Manager
	env      Env
	job      store.Job
	cfg      Config
	ctx      context.Context
	cancel   context.CancelFunc
	counters map[string]any
	gids     map[string]map[string]int64
	message  string
	progress int
}

func (r *run) inc(key string, n int) {
	v, _ := r.counters[key].(int)
	r.counters[key] = v + n
}

func (r *run) get(key string) int {
	v, _ := r.counters[key].(int)
	return v
}

func (r *run) save(progress int, msg string) {
	r.progress, r.message = progress, msg
	_ = r.env.Store.UpdateJob(r.ctx, r.job.ID, r.counters, progress, msg)
}

func (r *run) audit(kind, msg string, detail any) {
	r.env.Store.Audit(r.ctx, r.job.ID, kind, msg, detail)
}

func (r *run) cancelled() bool {
	return r.ctx.Err() != nil || r.env.Store.CancelRequested(r.ctx, r.job.ID)
}

func (r *run) finish(status, msg string) {
	if status == "" {
		status = "completed"
	}
	_ = r.env.Store.FinishJob(r.ctx, r.job.ID, status, r.counters, msg)
	r.env.Store.Audit(r.ctx, r.job.ID, "finished", label(r.job.Kind)+": "+msg, map[string]any{"status": status, "counters": r.counters})
}

// rowLink builds a link to a verified row; the tab's numeric ID is looked up
// once per job and failures only omit the anchor.
func (r *run) rowLink(conn *Conn, sheetID, tab string, row int64) string {
	if r.gids == nil {
		r.gids = map[string]map[string]int64{}
	}
	m, ok := r.gids[sheetID]
	if !ok {
		m, _ = conn.Sheets.SheetGIDs(r.ctx, sheetID)
		r.gids[sheetID] = m
	}
	gid, has := m[tab]
	return google.RowURL(r.cfg.Mode, sheetID, tab, gid, has, row)
}

func (r *run) fail(err error) {
	msg := netx.SafeMessage(err)
	r.finish("failed", msg)
}

func (r *run) execute(kind string, params map[string]any) {
	defer func() {
		if p := recover(); p != nil {
			r.env.Log.Error("job panic", "job", r.job.ID, "panic", fmt.Sprint(p))
			r.finish("failed", "Internal error; saved work was kept. Check the log and retry.")
		}
	}()
	switch kind {
	case "inbound_import":
		r.inboundImport(params)
	case "inbound_sync":
		r.inboundSyncJob(params)
	case "outbound_run":
		r.outboundRun(params)
	case "outbound_sync":
		r.outboundSyncJob(params)
	case "outbound_research":
		r.outboundResearch(params)
	}
}

func intParam(p map[string]any, key string, def, lo, hi int) int {
	v := def
	switch t := p[key].(type) {
	case float64:
		v = int(t)
	case int:
		v = t
	case int64:
		v = int(t)
	}
	return max(lo, min(hi, v))
}

func boolParam(p map[string]any, key string) bool {
	b, _ := p[key].(bool)
	return b
}

func idsParam(p map[string]any) []int64 {
	var out []int64
	switch t := p["ids"].(type) {
	case []any:
		for _, x := range t {
			if f, ok := x.(float64); ok {
				out = append(out, int64(f))
			}
		}
	case []int64:
		out = t
	}
	return out
}

func stringsParam(p map[string]any, key string) []string {
	var out []string
	switch t := p[key].(type) {
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		out = t
	}
	return out
}

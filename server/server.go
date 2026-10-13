// server.go assembles the application: database, vault, services, executor,
// scheduler and the HTTP router (API + SSE + embedded UI).
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flarco/g"
)

// Options configures the server.
type Options struct {
	DBPath            string
	Passphrase        string
	Token             string // optional bearer token for API clients (scripts, healthchecks)
	AdminUsername     string // admin account name (default "admin")
	AdminPassword     string // admin password set at deploy time; empty = keep stored or seed default
	Concurrency       int    // worker pool size (default 4)
	StreamConcurrency int    // replication stream parallelism (default 1)
	RetentionDays     int    // run/log retention (0 = keep forever)
	Version           string
}

// App is the assembled workbench server.
type App struct {
	DB          *DB
	Vault       *Vault
	Conns       *ConnService
	Broker      *Broker
	Router      *LogRouter
	Runner      *Runner
	Executor    *Executor
	Scheduler   *Scheduler
	Notifier    *Notifier
	Maintenance *Maintenance
	Sessions    *sessionStore

	LoginThrottle *loginThrottle

	Opt Options

	startedAt time.Time
}

// New builds and wires the app. Call Boot before serving.
func New(opt Options) (*App, error) {
	db, err := OpenDB(opt.DBPath)
	if err != nil {
		return nil, err
	}
	vault, err := NewVault(db, opt.Passphrase)
	if err != nil {
		return nil, err
	}

	broker := NewBroker()
	router := NewLogRouter(broker, db)
	notifier := NewNotifier(db, vault, broker)
	runner := NewRunner(db, broker, router, notifier)
	runner.StreamConcurrency = opt.StreamConcurrency
	executor := NewExecutor(db, runner, broker, notifier, opt.Concurrency)
	scheduler := NewScheduler(db, executor)
	conns := NewConnService(db, vault, broker)
	maintenance := NewMaintenance(db, notifier, opt.RetentionDays)

	app := &App{
		DB:            db,
		Vault:         vault,
		Conns:         conns,
		Broker:        broker,
		Router:        router,
		Runner:        runner,
		Executor:      executor,
		Scheduler:     scheduler,
		Notifier:      notifier,
		Maintenance:   maintenance,
		Sessions:      newSessionStore(),
		LoginThrottle: &loginThrottle{},
		Opt:           opt,
		startedAt:     time.Now(),
	}
	// ensure the admin account exists before any request can reach the API
	if err := app.bootstrapAdmin(); err != nil {
		return nil, err
	}
	return app, nil
}

// Boot starts background services after the HTTP stack is ready to serve.
func (a *App) Boot() error {
	a.Router.attach() // own the global log sink
	a.Executor.Start()
	a.Maintenance.Start()
	if err := a.Scheduler.Boot(); err != nil {
		return err
	}
	if err := a.Conns.Materialize(); err != nil {
		g.Warn("could not materialize connections: %s", err)
	}
	return nil
}

// Shutdown gracefully stops everything.
func (a *App) Shutdown() {
	a.Maintenance.Stop()
	a.Scheduler.Stop()
	a.Executor.Stop()
	a.Runner.CancelAll()
	// give in-flight runs a grace period to persist their final state
	deadline := time.Now().Add(8 * time.Second)
	for _, h := range a.Runner.Active() {
		select {
		case <-h.Done():
		case <-time.After(time.Until(deadline)):
			h.cancel()
		}
	}
}

/* --------------------------------- router -------------------------------- */

// Handler returns the full HTTP handler: API routes + SSE + embedded UI.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	// misc
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/version", a.handleVersion)
	mux.HandleFunc("GET /api/stats", a.handleStats)

	// auth
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("GET /api/auth/session", a.handleSession)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	mux.HandleFunc("POST /api/auth/password", a.handlePassword)

	// SSE
	mux.Handle("GET /api/events", a.Broker)

	// connections
	mux.HandleFunc("GET /api/conns", a.handleConnsList)
	mux.HandleFunc("GET /api/conns/types", a.handleConnTypes)
	mux.HandleFunc("GET /api/conns/export", a.handleConnsExport)
	mux.HandleFunc("GET /api/conns/{name}", a.handleConnsGet)
	mux.HandleFunc("POST /api/conns", a.handleConnsCreate)
	mux.HandleFunc("PUT /api/conns/{name}", a.handleConnsUpdate)
	mux.HandleFunc("DELETE /api/conns/{name}", a.handleConnsDelete)
	mux.HandleFunc("POST /api/conns/test", a.handleConnsTest)
	mux.HandleFunc("GET /api/conns/{name}/discover", a.handleConnsDiscover)
	mux.HandleFunc("GET /api/conns/{name}/preview", a.handleConnsPreview)
	mux.HandleFunc("POST /api/conns/{name}/primary-keys", a.handleConnsPrimaryKeys)
	mux.HandleFunc("POST /api/conns/import", a.handleConnsImport)

	// jobs
	mux.HandleFunc("GET /api/jobs", a.handleJobsList)
	mux.HandleFunc("POST /api/jobs", a.handleJobsCreate)
	mux.HandleFunc("GET /api/jobs/{id}", a.handleJobsGet)
	mux.HandleFunc("PUT /api/jobs/{id}", a.handleJobsUpdate)
	mux.HandleFunc("DELETE /api/jobs/{id}", a.handleJobsDelete)
	mux.HandleFunc("POST /api/jobs/{id}/run", a.handleJobsRun)
	mux.HandleFunc("GET /api/jobs/{id}/analyze", a.handleJobsAnalyze)
	mux.HandleFunc("POST /api/analyze", a.handleAnalyze)
	mux.HandleFunc("POST /api/jobs/{id}/duplicate", a.handleJobsDuplicate)
	mux.HandleFunc("GET /api/jobs/{id}/export", a.handleJobsExport)
	mux.HandleFunc("POST /api/jobs/import", a.handleJobsImport)
	mux.HandleFunc("GET /api/jobs/{id}/versions", a.handleJobVersions)
	mux.HandleFunc("POST /api/jobs/{id}/rollback", a.handleJobRollback)
	mux.HandleFunc("GET /api/jobs/{id}/next-runs", a.handleJobNextRuns)
	mux.HandleFunc("PATCH /api/jobs/{id}/schedule", a.handleJobSchedule)
	mux.HandleFunc("POST /api/jobs/batch-create", a.handleJobsBatchCreate)
	mux.HandleFunc("GET /api/schedules/upcoming", a.handleSchedulesUpcoming)

	// runs
	mux.HandleFunc("POST /api/runs", a.handleRunsCreate)
	mux.HandleFunc("GET /api/runs", a.handleRunsList)
	mux.HandleFunc("GET /api/runs/{id}", a.handleRunsGet)
	mux.HandleFunc("GET /api/runs/{id}/logs", a.handleRunLogs)
	mux.HandleFunc("POST /api/runs/{id}/cancel", a.handleRunCancel)

	// notifications
	mux.HandleFunc("GET /api/notifications", a.handleNotifications)
	mux.HandleFunc("POST /api/notifications/read", a.handleNotificationsRead)

	// notify channels (WeCom/Feishu/DingTalk/Slack/generic webhooks)
	mux.HandleFunc("GET /api/notify-channels", a.handleNotifyChannelsList)
	mux.HandleFunc("POST /api/notify-channels", a.handleNotifyChannelsCreate)
	mux.HandleFunc("PUT /api/notify-channels/{id}", a.handleNotifyChannelsUpdate)
	mux.HandleFunc("DELETE /api/notify-channels/{id}", a.handleNotifyChannelsDelete)
	mux.HandleFunc("POST /api/notify-channels/{id}/test", a.handleNotifyChannelTest)

	// settings / audit / backup
	mux.HandleFunc("GET /api/settings", a.handleSettingsGet)
	mux.HandleFunc("PUT /api/settings", a.handleSettingsPut)
	mux.HandleFunc("GET /api/audit", a.handleAudit)
	mux.HandleFunc("GET /api/backup", a.handleBackup)
	mux.HandleFunc("GET /api/backups", a.handleBackupsList)
	mux.HandleFunc("POST /api/backups", a.handleBackupsCreate)
	mux.HandleFunc("GET /api/backups/{name}/download", a.handleBackupDownload)
	mux.HandleFunc("DELETE /api/backups/{name}", a.handleBackupDelete)

	// embedded web UI (registered by web package via UIHandler hook)
	if ui := UIHandler(); ui != nil {
		mux.Handle("/", ui)
	}

	return a.middleware(mux)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush passes the optional Flusher interface through, so the SSE endpoint
// (/api/events) can stream; without it the type assertion in Broker.ServeHTTP
// fails and every EventSource connection 500s.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (a *App) middleware(next http.Handler) http.Handler {
	var reqID atomic.Int64
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := reqID.Add(1)
		rec := &statusRecorder{ResponseWriter: w, status: 200}

		// registered before the logging defer so suppression outlives the
		// [http] access line emitted at request end (defers run LIFO)
		restoreSuppress := a.Router.SuppressGID()
		defer restoreSuppress()

		defer func() {
			if p := recover(); p != nil {
				g.Debug("panic in %s %s: %v", r.Method, r.URL.Path, p)
				if rec.status == 200 {
					writeErr(rec, http.StatusInternalServerError, "internal error")
				}
			}
			if !strings.HasPrefix(r.URL.Path, "/api/events") {
				g.Debug("[http] #%d %s %s -> %d (%s)", id, r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
			}
		}()

		// authentication: session cookie (browser) or bearer token (API
		// clients). Exempt paths let the SPA shell load, logins start and
		// health checks answer without credentials.
		if !authExempt(r) {
			sess := a.sessionFrom(r)
			tokenOK := a.Opt.Token != "" && bearerToken(r) == a.Opt.Token
			if sess == nil && !tokenOK {
				writeErr(rec, http.StatusUnauthorized, "authentication required")
				return
			}
			// a session that still owes a password change may only change it
			// (or log out / probe itself); bearer-token calls are exempt
			if sess != nil && sess.MustChange && !authMustChangeAllowed(r) {
				writeErr(rec, http.StatusForbidden, "password change required")
				return
			}
			// CSRF defense-in-depth for cookie-authenticated mutations
			if sess != nil && crossSiteMutation(r) {
				writeErr(rec, http.StatusForbidden, "cross-site request rejected")
				return
			}
		}

		rec.Header().Set("X-Content-Type-Options", "nosniff")
		rec.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(rec, r)
	})
}

/* ------------------------------- helpers --------------------------------- */

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg, "status": status})
}

// apiErr maps service errors to HTTP responses.
func apiErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	// flarco/g errors render as "main cause | context | context"
	if i := strings.Index(msg, " => "); i > 0 {
		msg = msg[:i]
	}
	switch {
	case strings.Contains(err.Error(), "not found"):
		writeErr(w, http.StatusNotFound, msg)
	case strings.Contains(err.Error(), "already exists"),
		strings.Contains(err.Error(), "is required"),
		strings.Contains(err.Error(), "invalid"),
		strings.Contains(err.Error(), "used by jobs"):
		writeErr(w, http.StatusBadRequest, msg)
	default:
		writeErr(w, http.StatusInternalServerError, msg)
	}
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return g.Error(err, "invalid JSON body")
	}
	return nil
}

func pathVar(r *http.Request, name string) string {
	return r.PathValue(name)
}

// pathID parses the {id} path variable as an int64, writing a 400 on failure.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(pathVar(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

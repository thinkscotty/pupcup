// Package web is the PupCup HTTP layer: a net/http server serving the
// local-network app and a /healthz liveness probe. The web layer is plain
// request/response — it reads and writes domain state through the store and
// does not subscribe to the event bus (pages reflect state on load).
//
// Milestone 8 shipped the shell (server, base template, embedded statics,
// custom 404, /healthz). Milestone 9 adds the dashboard ("who's been fed
// today") and dogs management (create/update name·color·photo, soft-delete)
// plus photo serving. Pages are server-rendered HTML forms; the few non-GET/
// POST verbs in the route table (DELETE/PATCH) are reached from plain forms via
// methodOverride, so the app works without JavaScript. HTMX-driven feeding
// surfaces arrive in milestone 10.
package web

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/scottyturner/pupcup/internal/clock"
	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

// Deps are the collaborators a Server needs. All are required except Host.
type Deps struct {
	Store   *store.Store
	Log     *slog.Logger
	Clk     clock.Clock
	Version string
	// Loc renders timestamps in the household timezone and defines the local
	// "today" boundary the dashboard groups feedings by.
	Loc *time.Location
	// Host is an optional advertised address (e.g. "pupcup.local") shown in the
	// page header as a fallback for clients where mDNS doesn't resolve.
	Host string
	// PhotoDir is the directory dog photos are stored in and served from. Empty
	// disables uploads (they 4xx with a clear message) but the rest of the app
	// still runs.
	PhotoDir string
	// PhotoMaxKB / PhotoMaxPx bound uploaded photos (size and largest edge).
	PhotoMaxKB int
	PhotoMaxPx int
}

// Server holds the parsed templates and request handlers.
type Server struct {
	store      *store.Store
	log        *slog.Logger
	clk        clock.Clock
	loc        *time.Location
	version    string
	host       string
	photoDir   string
	photoMaxKB int
	photoMaxPx int
	startedAt  time.Time
	tmpl       *templates
	mux        *http.ServeMux
}

// New builds a Server with its routes and parsed templates.
func New(d Deps) (*Server, error) {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Clk == nil {
		d.Clk = clock.Real{}
	}
	if d.Loc == nil {
		d.Loc = time.Local
	}
	tmpl, err := newTemplates(d.Loc)
	if err != nil {
		return nil, err
	}
	s := &Server{
		store:      d.Store,
		log:        d.Log.With("component", "web.handler"),
		clk:        d.Clk,
		loc:        d.Loc,
		version:    d.Version,
		host:       d.Host,
		photoDir:   d.PhotoDir,
		photoMaxKB: d.PhotoMaxKB,
		photoMaxPx: d.PhotoMaxPx,
		startedAt:  d.Clk.Now(),
		tmpl:       tmpl,
	}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	mux := http.NewServeMux()

	// Embedded static assets, served under /static/ (the embed keeps the
	// "static/" prefix, so the FS path lines up with the URL path).
	mux.Handle("GET /static/", s.cacheStatic(http.FileServerFS(staticFS)))

	mux.HandleFunc("GET /{$}", s.handleDashboard)

	mux.HandleFunc("GET /dogs", s.handleDogsIndex)
	mux.HandleFunc("POST /dogs", s.handleDogCreate)
	mux.HandleFunc("POST /dogs/{id}", s.handleDogUpdate)
	mux.HandleFunc("DELETE /dogs/{id}", s.handleDogDelete)
	mux.HandleFunc("GET /photos/{id}", s.handlePhoto)

	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Catch-all: anything not matched above renders the 404 page.
	mux.HandleFunc("/", s.handleNotFound)

	s.mux = mux
}

// Handler returns the root http.Handler: request logging wraps method-override
// (which rewrites form-posted DELETE/PATCH before the mux matches on method).
func (s *Server) Handler() http.Handler {
	return s.logRequests(s.methodOverride(s.mux))
}

// baseData is the minimum every page template needs from the layout.
type baseData struct {
	Version string
	Host    string
	// Nav marks the active top-nav item ("dashboard" | "dogs").
	Nav string
}

func (s *Server) base(nav string) baseData {
	return baseData{Version: s.version, Host: s.host, Nav: nav}
}

// ----------------------------- dashboard ------------------------------------

// dashboardData drives the "who's been fed today" home page.
type dashboardData struct {
	baseData
	TodayLabel string
	Dogs       []dogStatus
	AnyDogs    bool
}

// dogStatus is one dog's feeding picture for the current local day.
type dogStatus struct {
	ID          int64
	Name        string
	AccentColor string
	HasPhoto    bool
	// Feedings today, chronological (oldest first).
	Feedings []feedingView
	// Last is the most recent feeding today, or nil if not fed yet.
	Last *feedingView
}

type feedingView struct {
	ID    int64
	TS    time.Time
	Score domain.Score
	Kind  domain.FeedKind
}

// handleDashboard renders per-dog status for the current local day: when each
// dog was last fed and how well it ate. Read-only — quick-add lands with the
// feeding surfaces in milestone 10. Errors degrade individual sections rather
// than failing the page (the dashboard is the household's at-a-glance view).
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	localNow := s.clk.Now().In(s.loc)
	startUTC := startOfLocalDay(localNow).UTC()

	data := dashboardData{
		baseData:   s.base("dashboard"),
		TodayLabel: localNow.Format("Monday, January 2"),
	}

	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		s.log.Error("dashboard list dogs", "err", err)
	}
	data.AnyDogs = len(dogs) > 0

	feeds, err := s.store.ListFeedings(ctx, store.FeedingFilter{Since: startUTC})
	if err != nil {
		s.log.Error("dashboard list feedings", "err", err)
	}
	byDog := make(map[int64][]domain.Feeding, len(dogs)) // newest-first per dog (store order)
	for _, f := range feeds {
		byDog[f.DogID] = append(byDog[f.DogID], f)
	}

	for _, d := range dogs {
		ds := dogStatus{ID: d.ID, Name: d.Name, AccentColor: d.AccentColor, HasPhoto: d.PhotoPath != ""}
		today := byDog[d.ID]
		if len(today) > 0 {
			last := toFeedingView(today[0])
			ds.Last = &last
			for i := len(today) - 1; i >= 0; i-- { // reverse to chronological
				ds.Feedings = append(ds.Feedings, toFeedingView(today[i]))
			}
		}
		data.Dogs = append(data.Dogs, ds)
	}

	if err := s.tmpl.render(w, http.StatusOK, "dashboard", data); err != nil {
		s.serverError(w, "dashboard", err)
	}
}

func toFeedingView(f domain.Feeding) feedingView {
	return feedingView{ID: f.ID, TS: f.TS, Score: f.Score, Kind: f.Kind}
}

// startOfLocalDay returns local midnight for the day containing t (t already in
// the target location).
func startOfLocalDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// ----------------------------- shell handlers -------------------------------

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	if err := s.tmpl.render(w, http.StatusNotFound, "404", s.base("")); err != nil {
		s.serverError(w, "404", err)
	}
}

func (s *Server) serverError(w http.ResponseWriter, where string, err error) {
	s.log.Error("render failed", "page", where, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// ----------------------------- middleware & helpers -------------------------

// methodOverride lets plain HTML forms reach the DELETE/PATCH routes the route
// table uses: a POST whose urlencoded body carries _method=DELETE|PATCH is
// rewritten before the mux matches on method. Only urlencoded POSTs are
// inspected, so multipart photo uploads (always plain POST) are never parsed
// here. This keeps the app fully functional without JavaScript; HTMX
// (milestone 10) issues the real verbs directly and bypasses this path.
func (s *Server) methodOverride(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost &&
			strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			switch strings.ToUpper(r.PostFormValue("_method")) {
			case http.MethodDelete:
				r.Method = http.MethodDelete
			case http.MethodPatch:
				r.Method = http.MethodPatch
			}
		}
		next.ServeHTTP(w, r)
	})
}

// cacheStatic adds a modest cache header to embedded assets.
func (s *Server) cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// flashRedirect performs a Post/Redirect/Get to path with a one-shot banner
// carried in the query string (level is "ok" or "err").
func (s *Server) flashRedirect(w http.ResponseWriter, r *http.Request, path, level, msg string) {
	q := url.Values{}
	q.Set("level", level)
	q.Set("flash", msg)
	http.Redirect(w, r, path+"?"+q.Encode(), http.StatusSeeOther)
}

// flash is a transient banner read from the query string after a redirect.
type flash struct {
	Level string // "ok" | "err"
	Msg   string
}

func readFlash(r *http.Request) *flash {
	msg := r.URL.Query().Get("flash")
	if msg == "" {
		return nil
	}
	level := r.URL.Query().Get("level")
	if level != "ok" && level != "err" {
		level = "ok"
	}
	return &flash{Level: level, Msg: msg}
}

// ----------------------------- request logging ------------------------------

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.clk.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		}
		s.log.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"latency_ms", s.clk.Now().Sub(start).Milliseconds(),
		)
	})
}

// Serve runs the HTTP server on addr until ctx is cancelled, then shuts it
// down gracefully (up to 5s for in-flight requests). Returns nil on a clean
// ctx-driven shutdown.
func (s *Server) Serve(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		s.log.Info("http listening", "addr", addr)
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}

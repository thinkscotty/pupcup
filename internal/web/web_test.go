package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/clock"
	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	// A temp-file DB (not ":memory:", which is shared-cache across Opens) keeps
	// each test isolated and gives SizeBytes a real, non-zero file to stat.
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv, err := New(Deps{
		Store:      st,
		Clk:        clock.Real{},
		Version:    "test-1.2.3",
		Host:       "pupcup.local",
		Loc:        time.UTC,
		PhotoDir:   t.TempDir(),
		PhotoMaxKB: 150,
		PhotoMaxPx: 320,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, st
}

func do(t *testing.T, srv *Server, method, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Result()
}

func TestHome_OK(t *testing.T) {
	srv, st := newTestServer(t)
	if _, err := st.CreateDog(context.Background(), domain.Dog{Name: "Riley", AccentColor: "#F2A6A1"}); err != nil {
		t.Fatalf("seed dog: %v", err)
	}

	resp := do(t, srv, http.MethodGet, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body := readBody(t, resp)
	for _, want := range []string{"PupCup", "Riley", "pupcup.local", "test-1.2.3"} {
		if !strings.Contains(body, want) {
			t.Errorf("home body missing %q", want)
		}
	}
}

func TestNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/does-not-exist")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "404") {
		t.Errorf("404 body missing %q marker: %s", "404", body)
	}
}

func TestStatic_CSS(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/static/app.css")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "css") {
		t.Fatalf("content-type = %q, want text/css", ct)
	}
	if body := readBody(t, resp); !strings.Contains(body, ":root") {
		t.Errorf("app.css body unexpected")
	}
}

func TestHealthz_Shape(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var h healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatalf("decode healthz: %v", err)
	}
	if !h.OK {
		t.Error("ok = false, want true")
	}
	if h.Version != "test-1.2.3" {
		t.Errorf("version = %q, want test-1.2.3", h.Version)
	}
	if h.DBSizeBytes <= 0 {
		t.Errorf("db_size_bytes = %d, want > 0", h.DBSizeBytes)
	}
	if h.DeviceLocked {
		t.Error("device_locked = true on a fresh DB, want false")
	}
	if h.LastButtonEvent != nil {
		t.Error("last_button_event should be nil with no button entries")
	}
}

func TestHealthz_ReflectsLockAndButtonEvent(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog, err := st.CreateDog(ctx, domain.Dog{Name: "Bard", AccentColor: "#A8C8F8"})
	if err != nil {
		t.Fatalf("seed dog: %v", err)
	}
	when := time.Now().Add(-30 * time.Minute).UTC().Truncate(time.Second)
	if _, err := st.CreateFeeding(ctx, domain.Feeding{
		DogID: dog.ID, TS: when, Kind: domain.FeedStandard,
		Score: domain.ScoreFull, Source: domain.SourceButton,
	}); err != nil {
		t.Fatalf("seed feeding: %v", err)
	}
	until := time.Now().Add(2 * time.Hour)
	if err := st.SetDeviceLock(ctx, domain.DeviceLock{Until: &until, Reason: "meal complete"}); err != nil {
		t.Fatalf("set lock: %v", err)
	}

	resp := do(t, srv, http.MethodGet, "/healthz")
	var h healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !h.DeviceLocked {
		t.Error("device_locked = false, want true")
	}
	if h.LockedUntil == nil {
		t.Fatal("locked_until = nil, want a time")
	}
	if h.LastButtonEvent == nil || !h.LastButtonEvent.Equal(when) {
		t.Errorf("last_button_event = %v, want %v", h.LastButtonEvent, when)
	}
}

func TestServe_ShutsDownOnContextCancel(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, "127.0.0.1:0") }()
	// Give the listener a moment, then cancel and expect a clean return.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil on ctx cancel", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

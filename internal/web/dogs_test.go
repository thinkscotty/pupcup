package web

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/clock"
	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

// ----------------------------- helpers --------------------------------------

// pngBytes encodes a solid w×h PNG.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{168, 200, 248, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return b.Bytes()
}

// multipartForm builds a multipart body with text fields and an optional file.
func multipartForm(t *testing.T, fields map[string]string, fileField, fileName string, fileData []byte) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	if fileData != nil {
		fw, err := w.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatalf("create file: %v", err)
		}
		if _, err := fw.Write(fileData); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &b, w.FormDataContentType()
}

func doBody(t *testing.T, srv *Server, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Result()
}

// postForm submits a urlencoded form (used to exercise method-override).
func postForm(t *testing.T, srv *Server, path string, vals url.Values) *http.Response {
	t.Helper()
	return doBody(t, srv, http.MethodPost, path, strings.NewReader(vals.Encode()), "application/x-www-form-urlencoded")
}

func locationOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (See Other)", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

// ----------------------------- dashboard ------------------------------------

func TestDashboard_TodayStatus(t *testing.T) {
	_, st := newTestServer(t) // st only; build a fake-clock server for determinism
	ctx := context.Background()

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	srv, err := New(Deps{
		Store: st, Clk: clock.NewFake(now), Loc: time.UTC, Version: "t",
		PhotoDir: t.TempDir(), PhotoMaxKB: 150, PhotoMaxPx: 320,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	fed, _ := st.CreateDog(ctx, domain.Dog{Name: "Fed", AccentColor: "#A8D8B9"})
	hungry, _ := st.CreateDog(ctx, domain.Dog{Name: "Hungry", AccentColor: "#F2A6A1"})
	stale, _ := st.CreateDog(ctx, domain.Dog{Name: "Stale", AccentColor: "#A8C8F8"})

	// Fed: a meal earlier today (08:00 UTC), full score.
	mustFeed(t, st, fed.ID, time.Date(2026, 6, 2, 8, 0, 0, 0, time.UTC), domain.ScoreFull)
	// Stale: only a meal yesterday — should read as not fed *today*.
	mustFeed(t, st, stale.ID, time.Date(2026, 6, 1, 19, 0, 0, 0, time.UTC), domain.ScoreFull)
	_ = hungry

	resp := do(t, srv, http.MethodGet, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)

	if !strings.Contains(body, "Cleaned the bowl") {
		t.Error("fed dog should show a full-meal label")
	}
	if !strings.Contains(body, "8:00 AM") {
		t.Errorf("fed dog should show the meal time; body:\n%s", body)
	}
	// Hungry and Stale both show the not-fed badge → at least two occurrences.
	if n := strings.Count(body, "Not fed yet"); n < 2 {
		t.Errorf("expected >=2 'Not fed yet' badges (Hungry + Stale), got %d", n)
	}
	if !strings.Contains(body, "June 2") {
		t.Error("dashboard should show today's date label")
	}
}

func mustFeed(t *testing.T, st *store.Store, dogID int64, ts time.Time, score domain.Score) {
	t.Helper()
	if _, err := st.CreateFeeding(context.Background(), domain.Feeding{
		DogID: dogID, TS: ts, Kind: domain.FeedStandard, Score: score, Source: domain.SourceWeb,
	}); err != nil {
		t.Fatalf("seed feeding: %v", err)
	}
}

// ----------------------------- dogs CRUD ------------------------------------

func TestDogCreate_Then_Listed(t *testing.T) {
	srv, st := newTestServer(t)
	body, ct := multipartForm(t, map[string]string{"name": "Pixel", "accent_color": "#A8C8F8"}, "", "", nil)
	resp := doBody(t, srv, http.MethodPost, "/dogs", body, ct)
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=ok") {
		t.Fatalf("expected ok flash, location = %q", loc)
	}

	dogs, _ := st.ListDogs(context.Background())
	if len(dogs) != 1 || dogs[0].Name != "Pixel" || dogs[0].AccentColor != "#A8C8F8" {
		t.Fatalf("dog not created as expected: %+v", dogs)
	}

	idx := do(t, srv, http.MethodGet, "/dogs")
	if b := readBody(t, idx); !strings.Contains(b, "Pixel") {
		t.Error("dogs index should list the new dog")
	}
}

func TestDogCreate_InvalidColorRejected(t *testing.T) {
	srv, st := newTestServer(t)
	body, ct := multipartForm(t, map[string]string{"name": "Bad", "accent_color": "blue"}, "", "", nil)
	resp := doBody(t, srv, http.MethodPost, "/dogs", body, ct)
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=err") {
		t.Fatalf("expected err flash for bad color, location = %q", loc)
	}
	if dogs, _ := st.ListDogs(context.Background()); len(dogs) != 0 {
		t.Errorf("no dog should be created on validation failure, got %d", len(dogs))
	}
}

func TestDogCreate_BlankNameRejected(t *testing.T) {
	srv, st := newTestServer(t)
	body, ct := multipartForm(t, map[string]string{"name": "   ", "accent_color": "#A8D8B9"}, "", "", nil)
	resp := doBody(t, srv, http.MethodPost, "/dogs", body, ct)
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=err") {
		t.Fatalf("expected err flash for blank name, location = %q", loc)
	}
	if dogs, _ := st.ListDogs(context.Background()); len(dogs) != 0 {
		t.Errorf("no dog should be created, got %d", len(dogs))
	}
}

func TestDogUpdate_RenameAndRecolor(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog, _ := st.CreateDog(ctx, domain.Dog{Name: "Old", AccentColor: "#A8D8B9"})

	body, ct := multipartForm(t, map[string]string{
		"name": "New", "accent_color": "#F8D8A0", "sort_order": "3",
	}, "", "", nil)
	resp := doBody(t, srv, http.MethodPost, "/dogs/"+itoa(dog.ID), body, ct)
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=ok") {
		t.Fatalf("expected ok flash, location = %q", loc)
	}

	got, _ := st.GetDog(ctx, dog.ID)
	if got.Name != "New" || got.AccentColor != "#F8D8A0" || got.SortOrder != 3 {
		t.Fatalf("update not applied: %+v", got)
	}
}

func TestDogDelete_OK_WhenNoHistory(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog, _ := st.CreateDog(ctx, domain.Dog{Name: "Temp", AccentColor: "#A8D8B9"})

	// method-override: a plain form POSTs _method=DELETE.
	resp := postForm(t, srv, "/dogs/"+itoa(dog.ID), url.Values{"_method": {"DELETE"}})
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=ok") {
		t.Fatalf("expected ok flash, location = %q", loc)
	}
	if dogs, _ := st.ListDogs(ctx); len(dogs) != 0 {
		t.Errorf("dog should be soft-deleted, still have %d", len(dogs))
	}
}

func TestDogDelete_BlockedByHistory(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog, _ := st.CreateDog(ctx, domain.Dog{Name: "Keeper", AccentColor: "#A8D8B9"})
	mustFeed(t, st, dog.ID, time.Now().UTC(), domain.ScoreFull)

	resp := postForm(t, srv, "/dogs/"+itoa(dog.ID), url.Values{"_method": {"DELETE"}})
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=err") {
		t.Fatalf("expected err flash (has history), location = %q", loc)
	}
	if dogs, _ := st.ListDogs(ctx); len(dogs) != 1 {
		t.Errorf("dog with history must remain, got %d", len(dogs))
	}
}

func TestDogsIndex_DisablesDeleteWithHistory(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog, _ := st.CreateDog(ctx, domain.Dog{Name: "Busy", AccentColor: "#A8D8B9"})
	mustFeed(t, st, dog.ID, time.Now().UTC(), domain.ScorePartial)

	body := readBody(t, do(t, srv, http.MethodGet, "/dogs"))
	if !strings.Contains(body, "disabled") {
		t.Error("delete button should be disabled for a dog with history")
	}
	if !strings.Contains(body, "can't be removed") {
		t.Error("index should explain why a dog can't be removed")
	}
}

// ----------------------------- photos ---------------------------------------

func TestPhoto_UploadAndServe(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()

	pngData := pngBytes(t, 64, 64)
	body, ct := multipartForm(t,
		map[string]string{"name": "Snap", "accent_color": "#A8C8F8"},
		"photo", "snap.png", pngData)
	resp := doBody(t, srv, http.MethodPost, "/dogs", body, ct)
	locationOf(t, resp)

	dogs, _ := st.ListDogs(ctx)
	if len(dogs) != 1 || dogs[0].PhotoPath == "" {
		t.Fatalf("dog should have a photo path: %+v", dogs)
	}
	// The stored file lives under photoDir as dog-<id>.png.
	if _, err := os.Stat(filepath.Join(srv.photoDir, dogs[0].PhotoPath)); err != nil {
		t.Fatalf("stored photo missing: %v", err)
	}

	pr := do(t, srv, http.MethodGet, "/photos/"+itoa(dogs[0].ID))
	if pr.StatusCode != http.StatusOK {
		t.Fatalf("photo status = %d, want 200", pr.StatusCode)
	}
	if ct := pr.Header.Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Errorf("photo content-type = %q, want image/png", ct)
	}
	served := readBodyBytes(t, pr)
	if len(served) != len(pngData) {
		t.Errorf("served %d bytes, stored %d", len(served), len(pngData))
	}
}

func TestPhoto_JPEGAccepted(t *testing.T) {
	srv, st := newTestServer(t)
	body, ct := multipartForm(t,
		map[string]string{"name": "Jay", "accent_color": "#F8D8A0"},
		"photo", "jay.jpg", jpegBytes(t, 100, 80))
	locationOf(t, doBody(t, srv, http.MethodPost, "/dogs", body, ct))

	dogs, _ := st.ListDogs(context.Background())
	if len(dogs) != 1 || !strings.HasSuffix(dogs[0].PhotoPath, ".jpg") {
		t.Fatalf("expected a .jpg photo: %+v", dogs)
	}
}

func TestPhoto_OversizedDimensionsRejected(t *testing.T) {
	srv, st := newTestServer(t) // PhotoMaxPx = 320
	body, ct := multipartForm(t,
		map[string]string{"name": "Big", "accent_color": "#A8D8B9"},
		"photo", "big.png", pngBytes(t, 400, 400))
	resp := doBody(t, srv, http.MethodPost, "/dogs", body, ct)
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=err") {
		t.Fatalf("expected err flash, location = %q", loc)
	}
	if dogs, _ := st.ListDogs(context.Background()); len(dogs) != 0 {
		t.Errorf("dog should not be created when the photo is rejected, got %d", len(dogs))
	}
}

func TestPhoto_NonImageRejected(t *testing.T) {
	srv, st := newTestServer(t)
	body, ct := multipartForm(t,
		map[string]string{"name": "Txt", "accent_color": "#A8D8B9"},
		"photo", "note.png", []byte("this is not a png"))
	resp := doBody(t, srv, http.MethodPost, "/dogs", body, ct)
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=err") {
		t.Fatalf("expected err flash, location = %q", loc)
	}
	if dogs, _ := st.ListDogs(context.Background()); len(dogs) != 0 {
		t.Errorf("dog should not be created, got %d", len(dogs))
	}
}

func TestPhoto_NotFoundWhenNone(t *testing.T) {
	srv, st := newTestServer(t)
	dog, _ := st.CreateDog(context.Background(), domain.Dog{Name: "Plain", AccentColor: "#A8D8B9"})

	if r := do(t, srv, http.MethodGet, "/photos/"+itoa(dog.ID)); r.StatusCode != http.StatusNotFound {
		t.Errorf("no-photo dog: status = %d, want 404", r.StatusCode)
	}
	if r := do(t, srv, http.MethodGet, "/photos/9999"); r.StatusCode != http.StatusNotFound {
		t.Errorf("missing dog: status = %d, want 404", r.StatusCode)
	}
}

func TestPhoto_RemoveClearsIt(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	// Create with a photo.
	body, ct := multipartForm(t,
		map[string]string{"name": "Clr", "accent_color": "#A8D8B9"},
		"photo", "c.png", pngBytes(t, 48, 48))
	locationOf(t, doBody(t, srv, http.MethodPost, "/dogs", body, ct))
	dogs, _ := st.ListDogs(ctx)
	id := dogs[0].ID

	// Update with remove_photo set and no new file.
	ub, uct := multipartForm(t, map[string]string{
		"name": "Clr", "accent_color": "#A8D8B9", "remove_photo": "1",
	}, "", "", nil)
	locationOf(t, doBody(t, srv, http.MethodPost, "/dogs/"+itoa(id), ub, uct))

	got, _ := st.GetDog(ctx, id)
	if got.PhotoPath != "" {
		t.Errorf("photo should be cleared, got %q", got.PhotoPath)
	}
	if r := do(t, srv, http.MethodGet, "/photos/"+itoa(id)); r.StatusCode != http.StatusNotFound {
		t.Errorf("after removal: status = %d, want 404", r.StatusCode)
	}
}

func TestSafeJoin_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"../etc/passwd", "../../secret", "/etc/passwd"} {
		got, err := safeJoin(dir, bad)
		if err != nil {
			continue // rejected outright — good
		}
		if !strings.HasPrefix(got, dir) {
			t.Errorf("safeJoin(%q) escaped dir: %q", bad, got)
		}
	}
	// A normal relative name resolves inside dir.
	got, err := safeJoin(dir, "dog-1.png")
	if err != nil || filepath.Dir(got) != dir {
		t.Errorf("safeJoin(dog-1.png) = %q, %v", got, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func readBodyBytes(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

// detailNow is a fixed "now" so window math (7/30/90 days back) is deterministic.
var detailNow = time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

func feedAt(t *testing.T, st *store.Store, dogID int64, ts time.Time, score domain.Score) {
	t.Helper()
	if _, err := st.CreateFeeding(context.Background(), domain.Feeding{
		DogID: dogID, TS: ts, Kind: domain.FeedStandard, Score: score, Source: domain.SourceWeb,
	}); err != nil {
		t.Fatalf("seed feeding: %v", err)
	}
}

func TestDogDetail_UnknownIDIs404(t *testing.T) {
	srv, _ := newTestServer(t)
	if resp := do(t, srv, http.MethodGet, "/dogs/999"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown dog: status = %d, want 404", resp.StatusCode)
	}
	if resp := do(t, srv, http.MethodGet, "/dogs/not-a-number"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("non-numeric id: status = %d, want 404", resp.StatusCode)
	}
}

func TestDogDetail_DeletedDogIs404(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Ghost")
	if err := st.SoftDeleteDog(context.Background(), dog.ID); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}
	if resp := do(t, srv, http.MethodGet, "/dogs/"+itoa(dog.ID)); resp.StatusCode != http.StatusNotFound {
		t.Errorf("soft-deleted dog: status = %d, want 404", resp.StatusCode)
	}
}

func TestDogDetail_RendersChartStatsAndTable(t *testing.T) {
	_, st := newTestServer(t)
	fsrv := fakeServer(t, st, detailNow, time.UTC)
	dog := seedDog(t, st, "Riley")

	// Four meals within the default 30-day window: 2 full, 1 partial, 1 none.
	feedAt(t, st, dog.ID, detailNow.AddDate(0, 0, -2), domain.ScoreFull)
	feedAt(t, st, dog.ID, detailNow.AddDate(0, 0, -3), domain.ScoreFull)
	feedAt(t, st, dog.ID, detailNow.AddDate(0, 0, -4), domain.ScorePartial)
	feedAt(t, st, dog.ID, detailNow.AddDate(0, 0, -5), domain.ScoreNone)
	if _, err := st.CreateSnack(context.Background(), domain.Snack{
		DogID: dog.ID, TS: detailNow.AddDate(0, 0, -1), Source: domain.SourceWeb,
	}); err != nil {
		t.Fatalf("seed snack: %v", err)
	}

	body := readBody(t, do(t, fsrv, http.MethodGet, "/dogs/"+itoa(dog.ID)))

	for _, want := range []string{
		"Riley",
		"<svg", `viewBox="0 0 720 220"`, // the chart
		"bar-full", "bar-partial", "bar-none", // every score is present
		`stat-num">4</span><span class="muted">meals`,  // summary total
		"full &middot; 50%",                            // 2/4
		`stat-num">1</span><span class="muted">snacks`, // snack tile
		"detail-table",                                 // history table
		"Cleaned the bowl",                             // a meal outcome label
		"window-tab active",                            // a window is selected
		"90d",                                          // all windows offered
		"full timeline",                                // link to /history
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
}

func TestDogDetail_WindowFiltersByRange(t *testing.T) {
	_, st := newTestServer(t)
	fsrv := fakeServer(t, st, detailNow, time.UTC)
	dog := seedDog(t, st, "Sunny")

	feedAt(t, st, dog.ID, detailNow.AddDate(0, 0, -2), domain.ScoreFull)  // in every window
	feedAt(t, st, dog.ID, detailNow.AddDate(0, 0, -40), domain.ScoreFull) // only in 90-day

	id := itoa(dog.ID)
	// Default (30 days): only the recent meal.
	if b := readBody(t, do(t, fsrv, http.MethodGet, "/dogs/"+id)); !strings.Contains(b, `stat-num">1</span><span class="muted">meals`) {
		t.Errorf("30-day window should count 1 meal; body: %s", b)
	}
	// 90 days: both meals.
	if b := readBody(t, do(t, fsrv, http.MethodGet, "/dogs/"+id+"?window=90")); !strings.Contains(b, `stat-num">2</span><span class="muted">meals`) {
		t.Errorf("90-day window should count 2 meals; body: %s", b)
	}
	// 7 days: still just the recent meal, and exactly one tab is marked active.
	b := readBody(t, do(t, fsrv, http.MethodGet, "/dogs/"+id+"?window=7"))
	if !strings.Contains(b, `stat-num">1</span><span class="muted">meals`) {
		t.Errorf("7-day window should count 1 meal; body: %s", b)
	}
	if n := strings.Count(b, "window-tab active"); n != 1 {
		t.Errorf("want exactly one active window tab, got %d", n)
	}
	// The active tab must be the requested 7-day one (its <a> carries the active
	// class on the line after its href).
	if !strings.Contains(b, `?window=7"`+"\n"+`       class="window-tab active`) {
		t.Errorf("7-day tab should be the active one; body: %s", b)
	}
}

func TestDogDetail_EmptyWindowShowsEmptyState(t *testing.T) {
	_, st := newTestServer(t)
	fsrv := fakeServer(t, st, detailNow, time.UTC)
	dog := seedDog(t, st, "Quiet")

	body := readBody(t, do(t, fsrv, http.MethodGet, "/dogs/"+itoa(dog.ID)))
	if !strings.Contains(body, "No meals recorded in this window") {
		t.Errorf("a dog with no meals should show the empty-state; body: %s", body)
	}
}

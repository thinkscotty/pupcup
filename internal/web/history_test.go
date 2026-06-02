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

// seedTimeline populates two dogs and one of each entry kind, returning the
// dogs. Feedings/snacks are instants; illness/stress are calendar dates.
func seedTimeline(t *testing.T, st *store.Store) (riley, sunny domain.Dog) {
	t.Helper()
	ctx := context.Background()
	riley = seedDog(t, st, "Riley")
	sunny = seedDog(t, st, "Sunny")

	if _, err := st.CreateFeeding(ctx, domain.Feeding{
		DogID: riley.ID, TS: time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
		Kind: domain.FeedStandard, Score: domain.ScoreFull, Source: domain.SourceWeb,
	}); err != nil {
		t.Fatalf("seed feeding: %v", err)
	}
	if _, err := st.CreateSnack(ctx, domain.Snack{
		DogID: riley.ID, TS: time.Date(2026, 5, 21, 15, 0, 0, 0, time.UTC), Source: domain.SourceWeb,
	}); err != nil {
		t.Fatalf("seed snack: %v", err)
	}
	end := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	if _, err := st.CreateIllness(ctx, domain.IllnessEvent{
		DogID: sunny.ID, Start: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), End: &end, Notes: "off food",
	}); err != nil {
		t.Fatalf("seed illness: %v", err)
	}
	if _, err := st.CreateStress(ctx, domain.StressEvent{
		Start: time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC), Kind: "thunderstorms",
	}); err != nil { // household (DogID nil)
		t.Fatalf("seed stress: %v", err)
	}
	return riley, sunny
}

func TestHistory_RendersMergedTimeline(t *testing.T) {
	srv, st := newTestServer(t)
	seedTimeline(t, st)

	resp := do(t, srv, http.MethodGet, "/history")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	for _, want := range []string{
		"Riley", "Sunny", "Whole household",
		"Meal", "Snack", "Illness", "Stress", "thunderstorms",
		"(4 entries)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("timeline missing %q", want)
		}
	}
}

func TestHistory_FilterByDog(t *testing.T) {
	srv, st := newTestServer(t)
	riley, _ := seedTimeline(t, st)

	// Riley has a meal + snack; the household stress applies to every dog, but
	// the illness belongs to Sunny and must drop out.
	resp := do(t, srv, http.MethodGet, "/history?dog="+itoa(riley.ID))
	body := readBody(t, resp)
	if !strings.Contains(body, "(3 entries)") {
		t.Errorf("filtered count wrong; body: %s", body)
	}
	// Assert on row content (Sunny still appears as a <select> option / the
	// category words appear in the type dropdown — only the timeline rows are
	// filtered).
	if strings.Contains(body, "<strong>Sunny</strong>") {
		t.Error("dog filter leaked another dog's row")
	}
	if strings.Contains(body, "off food") {
		t.Error("dog filter leaked another dog's illness row")
	}
	if !strings.Contains(body, "Whole household") {
		t.Error("household stress should show for any single dog")
	}
}

func TestHistory_FilterByType(t *testing.T) {
	srv, st := newTestServer(t)
	seedTimeline(t, st)

	resp := do(t, srv, http.MethodGet, "/history?type=feeding")
	body := readBody(t, resp)
	if !strings.Contains(body, "(1 entry)") {
		t.Errorf("type filter count wrong; body: %s", body)
	}
	if !strings.Contains(body, `badge-cat">Meal<`) {
		t.Error("type=feeding should show the meal row")
	}
	// The category words still appear in the type dropdown; assert no other
	// kind's row (category badge) is rendered.
	for _, gone := range []string{`badge-cat">Snack<`, `badge-cat">Illness<`, `badge-cat">Stress<`} {
		if strings.Contains(body, gone) {
			t.Errorf("type=feeding still shows a %q row", gone)
		}
	}
}

func TestHistory_FilterByDateRange(t *testing.T) {
	srv, st := newTestServer(t)
	seedTimeline(t, st)

	// Restrict to meals so only the feeding-instant bounds are under test.
	// The feeding is on 2026-05-20; the snack on 05-21.
	in := do(t, srv, http.MethodGet, "/history?type=feeding&from=2026-05-20&to=2026-05-20")
	if b := readBody(t, in); !strings.Contains(b, "(1 entry)") {
		t.Errorf("expected the 05-20 meal in range; body: %s", b)
	}
	out := do(t, srv, http.MethodGet, "/history?type=feeding&from=2026-05-21&to=2026-05-21")
	if b := readBody(t, out); !strings.Contains(b, "(0 entries)") {
		t.Errorf("05-20 meal should be out of a 05-21 window; body: %s", b)
	}
}

func TestHistory_EventOverlapsWindow(t *testing.T) {
	srv, st := newTestServer(t)
	seedTimeline(t, st)

	// The illness spans 05-10 → 05-15. A window fully inside it (05-12 → 05-13)
	// must still include it even though it started before the window opened.
	resp := do(t, srv, http.MethodGet, "/history?type=illness&from=2026-05-12&to=2026-05-13")
	body := readBody(t, resp)
	if !strings.Contains(body, "(1 entry)") {
		t.Errorf("illness overlapping the window should be included; body: %s", body)
	}
	if !strings.Contains(body, "off food") {
		t.Error("illness notes missing from overlapping match")
	}
}

func TestHistory_NoDogs(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/history")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if b := readBody(t, resp); !strings.Contains(b, "Add a dog") {
		t.Errorf("empty history should prompt to add a dog; body: %s", b)
	}
}

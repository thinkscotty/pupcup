package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/clock"
	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

// ----------------------------- helpers --------------------------------------

// fakeServer builds a Server over st with a fixed clock and location, so
// timestamp math is deterministic.
func fakeServer(t *testing.T, st *store.Store, now time.Time, loc *time.Location) *Server {
	t.Helper()
	srv, err := New(Deps{
		Store: st, Clk: clock.NewFake(now), Loc: loc, Version: "t",
		PhotoDir: t.TempDir(), PhotoMaxKB: 150, PhotoMaxPx: 320,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv
}

// hx issues a request with the HX-Request header set (urlencoded body when vals
// is non-nil), the way htmx submits forms and fires hx-get/patch/delete.
func hx(t *testing.T, srv *Server, method, path string, vals url.Values) *http.Response {
	t.Helper()
	var body io.Reader
	if vals != nil {
		body = strings.NewReader(vals.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if vals != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Result()
}

func seedDog(t *testing.T, st *store.Store, name string) domain.Dog {
	t.Helper()
	d, err := st.CreateDog(context.Background(), domain.Dog{Name: name, AccentColor: "#A8D8B9"})
	if err != nil {
		t.Fatalf("seed dog: %v", err)
	}
	return d
}

// ----------------------------- index ----------------------------------------

func TestFeedingsIndex_RendersFormsAndEntries(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Riley")
	mustFeed(t, st, dog.ID, time.Now().Add(-time.Hour).UTC(), domain.ScoreFull)

	body := readBody(t, do(t, srv, http.MethodGet, "/feedings"))
	for _, want := range []string{"Record a meal", "Quick snack", "Recent activity", "Riley", "Cleaned the bowl"} {
		if !strings.Contains(body, want) {
			t.Errorf("feedings index missing %q", want)
		}
	}
	_ = ctx
}

func TestRecentEntries_MergesAndOrders(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Bard")

	// A snack at 10:00, a meal at 09:00, a meal at 11:00 → expect 11:00, 10:00, 09:00.
	mustFeed(t, st, dog.ID, time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC), domain.ScoreFull)
	if _, err := st.CreateSnack(ctx, domain.Snack{DogID: dog.ID, TS: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC), Source: domain.SourceWeb}); err != nil {
		t.Fatalf("snack: %v", err)
	}
	mustFeed(t, st, dog.ID, time.Date(2026, 6, 2, 11, 0, 0, 0, time.UTC), domain.ScorePartial)

	entries, err := srv.recentEntries(ctx)
	if err != nil {
		t.Fatalf("recentEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if !(entries[0].TS.After(entries[1].TS) && entries[1].TS.After(entries[2].TS)) {
		t.Errorf("entries not in reverse-chronological order: %v", entries)
	}
	if entries[1].IsSnack != true {
		t.Errorf("middle entry should be the 10:00 snack, got %+v", entries[1])
	}
}

// ----------------------------- create ---------------------------------------

func TestFeedingCreate_HTMX_PrependsRow(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Pixel")

	resp := hx(t, srv, http.MethodPost, "/feedings", url.Values{
		"dog_id": {itoa(dog.ID)}, "score": {"full"}, "kind": {"standard"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Pixel") || !strings.Contains(body, "Cleaned the bowl") {
		t.Errorf("response should be the new entry row; got:\n%s", body)
	}
	// The OOB placeholder removal should be present.
	if !strings.Contains(body, `hx-swap-oob="delete"`) {
		t.Error("create response should carry the OOB empty-placeholder removal")
	}
	feeds, _ := st.ListFeedings(context.Background(), store.FeedingFilter{})
	if len(feeds) != 1 || feeds[0].Source != domain.SourceWeb {
		t.Fatalf("expected one web feeding, got %+v", feeds)
	}
}

func TestFeedingCreate_NonHTMX_Redirects(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Plain")

	resp := postForm(t, srv, "/feedings", url.Values{"dog_id": {itoa(dog.ID)}, "score": {"partial"}})
	if loc := locationOf(t, resp); !strings.Contains(loc, "level=ok") {
		t.Fatalf("expected ok PRG redirect, location = %q", loc)
	}
	if feeds, _ := st.ListFeedings(context.Background(), store.FeedingFilter{}); len(feeds) != 1 {
		t.Fatalf("feeding should be created on non-JS submit, got %d", len(feeds))
	}
}

func TestFeedingCreate_RetroactiveTimestamp(t *testing.T) {
	_, st := newTestServer(t)
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	srv := fakeServer(t, st, time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC), loc)
	dog := seedDog(t, st, "Retro")

	// 08:30 EDT on Jun 1 → 12:30 UTC.
	resp := hx(t, srv, http.MethodPost, "/feedings", url.Values{
		"dog_id": {itoa(dog.ID)}, "score": {"full"}, "ts": {"2026-06-01T08:30"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	feeds, _ := st.ListFeedings(context.Background(), store.FeedingFilter{})
	want := time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC)
	if len(feeds) != 1 || !feeds[0].TS.Equal(want) {
		t.Fatalf("retroactive ts = %v, want %v", feeds[0].TS, want)
	}
}

func TestFeedingCreate_InvalidScore_HTMXError(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Bad")

	resp := hx(t, srv, http.MethodPost, "/feedings", url.Values{"dog_id": {itoa(dog.ID)}, "score": {"banana"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (retargeted error)", resp.StatusCode)
	}
	if got := resp.Header.Get("HX-Retarget"); got != "#feed-msg" {
		t.Errorf("HX-Retarget = %q, want #feed-msg", got)
	}
	if body := readBody(t, resp); !strings.Contains(body, "flash-err") {
		t.Errorf("expected an error banner, got: %s", body)
	}
	if feeds, _ := st.ListFeedings(context.Background(), store.FeedingFilter{}); len(feeds) != 0 {
		t.Errorf("no feeding should be created on validation error, got %d", len(feeds))
	}
}

func TestFeedingCreate_QuickAddCard(t *testing.T) {
	_, st := newTestServer(t)
	srv := fakeServer(t, st, time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC), time.UTC)
	dog := seedDog(t, st, "Quick")

	resp := hx(t, srv, http.MethodPost, "/feedings", url.Values{
		"dog_id": {itoa(dog.ID)}, "score": {"full"}, "return": {"card"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "dog-card-"+itoa(dog.ID)) {
		t.Errorf("quick-add should return the refreshed dog card; got:\n%s", body)
	}
	if !strings.Contains(body, "Cleaned the bowl") {
		t.Error("refreshed card should reflect the new full meal")
	}
	if feeds, _ := st.ListFeedings(context.Background(), store.FeedingFilter{DogID: dog.ID}); len(feeds) != 1 {
		t.Fatalf("quick-add should record one feeding, got %d", len(feeds))
	}
}

// ----------------------------- edit / update --------------------------------

func TestFeedingEditForm_PrefillsAndComparesScore(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Edit")
	fd, _ := st.CreateFeeding(ctx, domain.Feeding{
		DogID: dog.ID, TS: time.Now().UTC(), Kind: domain.FeedNonstandard,
		Score: domain.ScorePartial, Source: domain.SourceWeb,
	})

	body := readBody(t, hx(t, srv, http.MethodGet, "/feedings/"+itoa(fd.ID)+"/edit", nil))
	if !strings.Contains(body, `hx-patch="/feedings/`+itoa(fd.ID)+`"`) {
		t.Error("edit form should PATCH the feeding")
	}
	// eq on the named Score/Kind types must mark the right options selected.
	if !strings.Contains(body, `value="partial" selected`) {
		t.Errorf("score 'partial' should be preselected; got:\n%s", body)
	}
	if !strings.Contains(body, `value="nonstandard" selected`) {
		t.Error("kind 'nonstandard' should be preselected")
	}
}

func TestFeedingUpdate_AppliesAndMarksEdited(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Upd")
	fd, _ := st.CreateFeeding(ctx, domain.Feeding{
		DogID: dog.ID, TS: time.Now().UTC(), Kind: domain.FeedStandard,
		Score: domain.ScoreFull, Source: domain.SourceWeb,
	})

	resp := hx(t, srv, http.MethodPatch, "/feedings/"+itoa(fd.ID), url.Values{
		"score": {"none"}, "kind": {"standard"}, "ts": {"2026-06-01T07:00"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "badge score-none") {
		t.Errorf("update response should be the refreshed row scored 'none'; got:\n%s", body)
	}
	got, _ := st.GetFeeding(ctx, fd.ID)
	if got.Score != domain.ScoreNone {
		t.Errorf("score = %q, want none", got.Score)
	}
	if got.EditedAt == nil {
		t.Error("EditedAt should be set after an update")
	}
}

func TestFeedingRow_CancelReturnsRow(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Cancel")
	fd, _ := st.CreateFeeding(context.Background(), domain.Feeding{
		DogID: dog.ID, TS: time.Now().UTC(), Kind: domain.FeedStandard,
		Score: domain.ScoreFull, Source: domain.SourceWeb,
	})
	body := readBody(t, hx(t, srv, http.MethodGet, "/feedings/"+itoa(fd.ID), nil))
	if !strings.Contains(body, `id="feeding-`+itoa(fd.ID)+`"`) || !strings.Contains(body, "Cleaned the bowl") {
		t.Errorf("cancel should restore the read-only row; got:\n%s", body)
	}
}

func TestFeedingDelete_SoftDeletes(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Del")
	fd, _ := st.CreateFeeding(ctx, domain.Feeding{
		DogID: dog.ID, TS: time.Now().UTC(), Kind: domain.FeedStandard,
		Score: domain.ScoreFull, Source: domain.SourceWeb,
	})
	resp := hx(t, srv, http.MethodDelete, "/feedings/"+itoa(fd.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if b := readBody(t, resp); strings.TrimSpace(b) != "" {
		t.Errorf("delete should return an empty body, got %q", b)
	}
	if feeds, _ := st.ListFeedings(ctx, store.FeedingFilter{}); len(feeds) != 0 {
		t.Errorf("feeding should be soft-deleted, %d remain", len(feeds))
	}
}

// ----------------------------- snacks ---------------------------------------

func TestSnackCreate_HTMX(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Snacker")

	resp := hx(t, srv, http.MethodPost, "/snacks", url.Values{
		"dog_id": {itoa(dog.ID)}, "specifics": {"dental chew"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Snack") || !strings.Contains(body, "dental chew") {
		t.Errorf("response should be the new snack row; got:\n%s", body)
	}
	if snacks, _ := st.ListSnacks(context.Background(), store.SnackFilter{}); len(snacks) != 1 {
		t.Fatalf("expected one snack, got %d", len(snacks))
	}
}

func TestSnackUpdateAndDelete(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "S2")
	sn, _ := st.CreateSnack(ctx, domain.Snack{DogID: dog.ID, TS: time.Now().UTC(), Source: domain.SourceWeb})

	// Update notes.
	resp := hx(t, srv, http.MethodPatch, "/snacks/"+itoa(sn.ID), url.Values{
		"ts": {"2026-06-01T07:00"}, "specifics": {"liver"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	got, _ := st.GetSnack(ctx, sn.ID)
	if got.Specifics != "liver" || got.EditedAt == nil {
		t.Errorf("snack not updated as expected: %+v", got)
	}

	// Delete.
	if resp := hx(t, srv, http.MethodDelete, "/snacks/"+itoa(sn.ID), nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	if snacks, _ := st.ListSnacks(ctx, store.SnackFilter{}); len(snacks) != 0 {
		t.Errorf("snack should be soft-deleted, %d remain", len(snacks))
	}
}

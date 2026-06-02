package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

// fixedNow is the deterministic clock these tests run against.
var fixedNow = time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

// ----------------------------- illness --------------------------------------

func TestIllnessIndex_RendersFormAndEvents(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Riley")
	if _, err := st.CreateIllness(ctx, domain.IllnessEvent{
		DogID: dog.ID, Start: time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC), Notes: "off food",
	}); err != nil {
		t.Fatalf("seed illness: %v", err)
	}

	body := readBody(t, do(t, srv, http.MethodGet, "/illness"))
	for _, want := range []string{"Log an illness", "Illness history", "Riley", "off food", "Ongoing", "May 30 2026"} {
		if !strings.Contains(body, want) {
			t.Errorf("illness index missing %q", want)
		}
	}
}

func TestIllnessCreate_HTMX_PrependsOngoingRow(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Bentley")

	resp := hx(t, srv, http.MethodPost, "/illness", url.Values{
		"dog_id":  {itoa(dog.ID)},
		"start":   {"2026-06-01"},
		"ongoing": {"1"},
		"notes":   {"limping"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	for _, want := range []string{`id="illness-1"`, "Bentley", "Ongoing", "Jun 1 2026", "limping", `id="illness-empty"`, "hx-swap-oob"} {
		if !strings.Contains(body, want) {
			t.Errorf("create row missing %q\n%s", want, body)
		}
	}
	// Persisted as ongoing (no end).
	ev, err := st.GetIllness(context.Background(), 1)
	if err != nil {
		t.Fatalf("get illness: %v", err)
	}
	if ev.End != nil {
		t.Errorf("End = %v, want nil (ongoing)", ev.End)
	}
}

func TestIllnessCreate_NonHTMX_Redirects(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Bard")

	resp := postForm(t, srv, "/illness", url.Values{
		"dog_id":  {itoa(dog.ID)},
		"start":   {"2026-06-02"},
		"ongoing": {"1"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if loc := locationOf(t, resp); !strings.HasPrefix(loc, "/illness?") || !strings.Contains(loc, "flash=") {
		t.Errorf("redirect = %q, want /illness?...flash=", loc)
	}
	events, _ := st.ListIllness(context.Background(), 0)
	if len(events) != 1 {
		t.Fatalf("got %d illness events, want 1", len(events))
	}
}

func TestIllnessCreate_NoDog_HTMXError(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := hx(t, srv, http.MethodPost, "/illness", url.Values{
		"start":   {"2026-06-02"},
		"ongoing": {"1"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("HX-Retarget"); got != "#illness-msg" {
		t.Errorf("HX-Retarget = %q, want #illness-msg", got)
	}
	if !strings.Contains(readBody(t, resp), "flash-err") {
		t.Errorf("expected an error banner")
	}
}

func TestIllnessSetEnd_PATCH(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Riley")
	ev, err := st.CreateIllness(ctx, domain.IllnessEvent{
		DogID: dog.ID, Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Notes: "tummy",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The ongoing-row "set end" form posts the unchanged start/notes plus an end.
	resp := hx(t, srv, http.MethodPatch, "/illness/"+itoa(ev.ID), url.Values{
		"start": {"2026-06-01"},
		"notes": {"tummy"},
		"end":   {"2026-06-05"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	if strings.Contains(body, "Ongoing") {
		t.Errorf("row still shows Ongoing after setting an end:\n%s", body)
	}
	if !strings.Contains(body, "Jun 5 2026") {
		t.Errorf("row missing end date Jun 5 2026:\n%s", body)
	}
	got, _ := st.GetIllness(ctx, ev.ID)
	if got.End == nil {
		t.Fatalf("End not persisted")
	}
	if y, m, d := got.End.UTC().Date(); y != 2026 || m != time.June || d != 5 {
		t.Errorf("End = %v, want 2026-06-05", got.End)
	}
}

func TestIllnessUpdate_OngoingTogglesEndOff(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Riley")
	end := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	ev, err := st.CreateIllness(ctx, domain.IllnessEvent{
		DogID: dog.ID, Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), End: &end,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Re-mark ongoing: ongoing toggle set, an end value present but ignored.
	resp := hx(t, srv, http.MethodPatch, "/illness/"+itoa(ev.ID), url.Values{
		"start":   {"2026-06-01"},
		"ongoing": {"1"},
		"end":     {"2026-06-05"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, _ := st.GetIllness(ctx, ev.ID)
	if got.End != nil {
		t.Errorf("End = %v, want nil after ongoing toggle", got.End)
	}
}

func TestIllnessEndBeforeStart_Error(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Riley")
	resp := hx(t, srv, http.MethodPost, "/illness", url.Values{
		"dog_id": {itoa(dog.ID)},
		"start":  {"2026-06-10"},
		"end":    {"2026-06-01"},
	})
	if got := resp.Header.Get("HX-Retarget"); got != "#illness-msg" {
		t.Errorf("HX-Retarget = %q, want #illness-msg", got)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "before the start") {
		t.Errorf("expected end-before-start error, got:\n%s", body)
	}
	if events, _ := st.ListIllness(context.Background(), 0); len(events) != 0 {
		t.Errorf("event was saved despite validation error")
	}
}

func TestIllnessRow_CancelReturnsRow(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Riley")
	end := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	ev, _ := st.CreateIllness(context.Background(), domain.IllnessEvent{
		DogID: dog.ID, Start: fixedNow, End: &end,
	})
	body := readBody(t, hx(t, srv, http.MethodGet, "/illness/"+itoa(ev.ID), nil))
	// The read-only row carries the Edit button but not the edit form's message
	// slot or Save button.
	if !strings.Contains(body, `id="illness-`+itoa(ev.ID)+`"`) ||
		!strings.Contains(body, "/illness/"+itoa(ev.ID)+"/edit") ||
		strings.Contains(body, "edit-msg-illness") {
		t.Errorf("cancel did not return a read-only row:\n%s", body)
	}
}

func TestIllnessDelete(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Riley")
	ev, _ := st.CreateIllness(context.Background(), domain.IllnessEvent{DogID: dog.ID, Start: fixedNow})

	resp := hx(t, srv, http.MethodDelete, "/illness/"+itoa(ev.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if events, _ := st.ListIllness(context.Background(), 0); len(events) != 0 {
		t.Errorf("event not deleted")
	}
}

// TestEventDate_NoTimezoneShift guards the date-only handling: a calendar date
// must round-trip and display unchanged even under a far-east timezone, where a
// naive local→UTC conversion would roll the day backwards.
func TestEventDate_NoTimezoneShift(t *testing.T) {
	srv, st := newTestServer(t)
	srv.loc = time.FixedZone("JST", 9*3600) // +09:00
	dog := seedDog(t, st, "Riley")

	resp := hx(t, srv, http.MethodPost, "/illness", url.Values{
		"dog_id":  {itoa(dog.ID)},
		"start":   {"2026-06-15"},
		"ongoing": {"1"},
	})
	body := readBody(t, resp)
	if !strings.Contains(body, "Jun 15 2026") {
		t.Errorf("date shifted; row missing Jun 15 2026:\n%s", body)
	}
	if strings.Contains(body, "Jun 14 2026") {
		t.Errorf("date rolled back a day under +09:00")
	}
}

// ----------------------------- stress ---------------------------------------

func TestStressCreate_Household_HTMX(t *testing.T) {
	srv, st := newTestServer(t)
	resp := hx(t, srv, http.MethodPost, "/stress", url.Values{
		"dog_id":  {""}, // whole household
		"kind":    {"travel"},
		"start":   {"2026-06-01"},
		"ongoing": {"1"},
		"notes":   {"road trip"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	for _, want := range []string{"Whole household", "Household", "travel", "Ongoing", "road trip"} {
		if !strings.Contains(body, want) {
			t.Errorf("household stress row missing %q\n%s", want, body)
		}
	}
	ev, err := st.GetStress(context.Background(), 1)
	if err != nil {
		t.Fatalf("get stress: %v", err)
	}
	if ev.DogID != nil {
		t.Errorf("DogID = %v, want nil (household)", ev.DogID)
	}
}

func TestStressCreate_ForDog(t *testing.T) {
	srv, st := newTestServer(t)
	dog := seedDog(t, st, "Bentley")
	resp := hx(t, srv, http.MethodPost, "/stress", url.Values{
		"dog_id": {itoa(dog.ID)},
		"kind":   {"vet visit"},
		"start":  {"2026-06-02"},
		"end":    {"2026-06-02"},
	})
	body := readBody(t, resp)
	if !strings.Contains(body, "Bentley") || strings.Contains(body, "Household") {
		t.Errorf("per-dog stress row wrong subject:\n%s", body)
	}
	ev, _ := st.GetStress(context.Background(), 1)
	if ev.DogID == nil || *ev.DogID != dog.ID {
		t.Errorf("DogID = %v, want %d", ev.DogID, dog.ID)
	}
	if ev.End == nil {
		t.Errorf("End not set")
	}
}

func TestStressUpdate_ChangesSubject(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Riley")
	ev, _ := st.CreateStress(ctx, domain.StressEvent{Start: fixedNow, Kind: "storms"}) // household

	resp := hx(t, srv, http.MethodPatch, "/stress/"+itoa(ev.ID), url.Values{
		"dog_id": {itoa(dog.ID)},
		"kind":   {"storms"},
		"start":  {"2026-06-02"},
		"end":    {"2026-06-03"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, _ := st.GetStress(ctx, ev.ID)
	if got.DogID == nil || *got.DogID != dog.ID {
		t.Errorf("subject not changed to dog %d: %v", dog.ID, got.DogID)
	}
	if got.End == nil {
		t.Errorf("end not set")
	}
}

func TestStressEditForm_SelectsCurrentSubject(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	dog := seedDog(t, st, "Riley")
	did := dog.ID
	ev, _ := st.CreateStress(ctx, domain.StressEvent{DogID: &did, Start: fixedNow})

	body := readBody(t, hx(t, srv, http.MethodGet, "/stress/"+itoa(ev.ID)+"/edit", nil))
	if !strings.Contains(body, `value="`+itoa(did)+`" selected`) {
		t.Errorf("edit form did not pre-select current dog:\n%s", body)
	}
	if !strings.Contains(body, "hx-patch") {
		t.Errorf("edit form missing hx-patch")
	}
}

func TestStressDelete(t *testing.T) {
	srv, st := newTestServer(t)
	ev, _ := st.CreateStress(context.Background(), domain.StressEvent{Start: fixedNow})
	resp := hx(t, srv, http.MethodDelete, "/stress/"+itoa(ev.ID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if events, _ := st.ListStress(context.Background(), 0); len(events) != 0 {
		t.Errorf("stress event not deleted")
	}
}

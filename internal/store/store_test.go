package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrationsApplied(t *testing.T) {
	s := newTestStore(t)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected >= 1 migration applied, got %d", n)
	}
	// Re-running migrations is a no-op.
	if err := s.migrate(); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
}

func TestDogCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	d, err := s.CreateDog(ctx, domain.Dog{Name: "Cleo", AccentColor: "#A8D8B9"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == 0 {
		t.Fatal("expected id populated")
	}
	got, err := s.GetDog(ctx, d.ID)
	if err != nil || got.Name != "Cleo" {
		t.Fatalf("get: %v %+v", err, got)
	}

	got.Name = "Cleopatra"
	if err := s.UpdateDog(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetDog(ctx, d.ID)
	if got2.Name != "Cleopatra" {
		t.Fatalf("rename failed: %+v", got2)
	}

	dogs, err := s.ListDogs(ctx)
	if err != nil || len(dogs) != 1 {
		t.Fatalf("list: err=%v n=%d", err, len(dogs))
	}

	if err := s.SoftDeleteDog(ctx, d.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if dogs, _ := s.ListDogs(ctx); len(dogs) != 0 {
		t.Fatal("expected dog soft-deleted")
	}
}

func TestSoftDeleteDog_BlockedByEntries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	d, _ := s.CreateDog(ctx, domain.Dog{Name: "Pip", AccentColor: "#A8D8B9"})
	if _, err := s.CreateFeeding(ctx, domain.Feeding{
		DogID: d.ID, TS: time.Now(), Kind: domain.FeedStandard, Score: domain.ScoreFull, Source: domain.SourceButton,
	}); err != nil {
		t.Fatal(err)
	}
	err := s.SoftDeleteDog(ctx, d.ID)
	if err == nil || !strings.Contains(err.Error(), "active entries") {
		t.Fatalf("expected block, got %v", err)
	}
}

func TestFeedingCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	d, _ := s.CreateDog(ctx, domain.Dog{Name: "Rio", AccentColor: "#F8D8A0"})

	now := time.Date(2026, 4, 29, 8, 0, 0, 0, time.UTC)
	f, err := s.CreateFeeding(ctx, domain.Feeding{
		DogID: d.ID, TS: now, Kind: domain.FeedStandard, Score: domain.ScoreFull, Source: domain.SourceButton,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !f.TS.Equal(now) {
		t.Fatalf("ts roundtrip: got %v want %v", f.TS, now)
	}
	if f.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not populated")
	}

	// Update score.
	f.Score = domain.ScorePartial
	if err := s.UpdateFeeding(ctx, f); err != nil {
		t.Fatal(err)
	}
	f2, _ := s.GetFeeding(ctx, f.ID)
	if f2.Score != domain.ScorePartial || f2.EditedAt == nil {
		t.Fatalf("update: %+v", f2)
	}

	// Filter by dog + window.
	got, err := s.ListFeedings(ctx, FeedingFilter{DogID: d.ID, Since: now.Add(-time.Hour), Until: now.Add(time.Hour)})
	if err != nil || len(got) != 1 {
		t.Fatalf("filter: err=%v n=%d", err, len(got))
	}
	got2, err := s.ListFeedings(ctx, FeedingFilter{DogID: d.ID, Since: now.Add(time.Hour)})
	if err != nil || len(got2) != 0 {
		t.Fatalf("expected 0 outside window, got %d", len(got2))
	}

	// Soft-delete.
	if err := s.SoftDeleteFeeding(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListFeedings(ctx, FeedingFilter{}); len(got) != 0 {
		t.Fatal("soft-deleted feeding listed")
	}
	// Still retrievable via Get (so handlers can show edit-history).
	gone, err := s.GetFeeding(ctx, f.ID)
	if err != nil || gone.DeletedAt == nil {
		t.Fatalf("get after delete: %v %+v", err, gone)
	}
}

func TestSnackCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	d, _ := s.CreateDog(ctx, domain.Dog{Name: "Otis", AccentColor: "#A8C8F8"})

	now := time.Now().UTC().Truncate(time.Millisecond)
	sn, err := s.CreateSnack(ctx, domain.Snack{DogID: d.ID, TS: now, Source: domain.SourceButton, Specifics: "carrot"})
	if err != nil {
		t.Fatal(err)
	}
	if sn.Specifics != "carrot" {
		t.Fatalf("specifics: %q", sn.Specifics)
	}
	if err := s.SoftDeleteSnack(ctx, sn.ID); err != nil {
		t.Fatal(err)
	}
}

func TestIllnessCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	d, _ := s.CreateDog(ctx, domain.Dog{Name: "Dotty", AccentColor: "#F2A6A1"})

	start := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	e, err := s.CreateIllness(ctx, domain.IllnessEvent{DogID: d.ID, Start: start, Notes: "tummy upset"})
	if err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	e.End = &end
	if err := s.UpdateIllness(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetIllness(ctx, e.ID)
	if got.End == nil || !got.End.Equal(end) {
		t.Fatalf("end: %+v", got)
	}
	if err := s.DeleteIllness(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStressCRUD_HouseholdWide(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	start := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	e, err := s.CreateStress(ctx, domain.StressEvent{Start: start, Kind: "houseguests"})
	if err != nil {
		t.Fatal(err)
	}
	if e.DogID != nil {
		t.Fatal("expected nil dog (household-wide)")
	}
}

func TestDeviceLockRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	lock, err := s.GetDeviceLock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Until != nil {
		t.Fatal("fresh DB should have no lock")
	}

	until := time.Date(2026, 4, 29, 12, 30, 0, 0, time.UTC)
	if err := s.SetDeviceLock(ctx, domain.DeviceLock{Until: &until, Reason: "meal"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetDeviceLock(ctx)
	if got.Until == nil || !got.Until.Equal(until) || got.Reason != "meal" {
		t.Fatalf("got %+v", got)
	}

	if err := s.SetDeviceLock(ctx, domain.DeviceLock{}); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetDeviceLock(ctx)
	if got2.Until != nil {
		t.Fatal("expected cleared lock")
	}
}

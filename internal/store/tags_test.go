package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

func TestTags_SeedAndUnspecifiedSentinel(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tags, err := s.ListTags(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	// 1 sentinel + 8 starter tags from migration 0002.
	if len(tags) != 9 {
		t.Fatalf("want 9 seeded tags, got %d", len(tags))
	}
	uns, err := s.GetTag(ctx, UnspecifiedTagID)
	if err != nil {
		t.Fatal(err)
	}
	if !uns.IsUnspecified {
		t.Errorf("tag %d should be the Unspecified sentinel", UnspecifiedTagID)
	}
	// The sentinel may not be renamed or archived.
	if err := s.RenameTag(ctx, UnspecifiedTagID, "Nope"); err == nil {
		t.Error("renaming the sentinel should fail")
	}
	if err := s.ArchiveTag(ctx, UnspecifiedTagID); err == nil {
		t.Error("archiving the sentinel should fail")
	}
}

func TestTags_CreateCanonAndUniqueness(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tag, err := s.CreateTag(ctx, "  pumpkin   purée ")
	if err != nil {
		t.Fatal(err)
	}
	if tag.Name != "Pumpkin Purée" {
		t.Errorf("canonicalized name = %q, want %q", tag.Name, "Pumpkin Purée")
	}
	// Case-insensitive collision among live tags.
	if _, err := s.CreateTag(ctx, "pumpkin purée"); !errors.Is(err, ErrTagNameTaken) {
		t.Errorf("duplicate create err = %v, want ErrTagNameTaken", err)
	}
	// GetOrCreate returns the existing one rather than erroring.
	got, err := s.GetOrCreateTag(ctx, "Pumpkin Purée")
	if err != nil || got.ID != tag.ID {
		t.Fatalf("GetOrCreate existing: id=%d err=%v (want id=%d)", got.ID, err, tag.ID)
	}
}

func TestTags_ArchiveHidesFromCatalog(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tag, err := s.CreateTag(ctx, "Sardines")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveTag(ctx, tag.ID); err != nil {
		t.Fatal(err)
	}
	live, _ := s.ListTags(ctx, false)
	for _, tg := range live {
		if tg.ID == tag.ID {
			t.Error("archived tag should be hidden from the live catalog")
		}
	}
	all, _ := s.ListTags(ctx, true)
	found := false
	for _, tg := range all {
		if tg.ID == tag.ID {
			found = true
		}
	}
	if !found {
		t.Error("archived tag should still appear when includeArchived is set")
	}
	// A new live tag may reuse the archived name (partial unique index).
	if _, err := s.CreateTag(ctx, "Sardines"); err != nil {
		t.Errorf("reusing an archived name should be allowed: %v", err)
	}
}

func TestTags_AttachDetachAndFeedingLoad(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	dog := mustDog(t, s, "Riley")

	feed, err := s.CreateFeeding(ctx, domain.Feeding{
		DogID: dog.ID, TS: time.Now(), Kind: domain.FeedStandard,
		Score: domain.ScoreFull, Source: domain.SourceButton,
	})
	if err != nil {
		t.Fatal(err)
	}
	cheese, _ := s.CreateTag(ctx, "Cheese-extra") // distinct from seed
	chicken, _ := s.GetOrCreateTag(ctx, "Shredded Chicken")

	if err := s.AttachTag(ctx, feed.ID, cheese.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachTag(ctx, feed.ID, cheese.ID); err != nil {
		t.Fatalf("re-attach should be idempotent: %v", err)
	}
	if err := s.AttachTag(ctx, feed.ID, chicken.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetFeeding(ctx, feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("feeding should carry 2 tags, got %d", len(got.Tags))
	}

	// ListFeedings also populates tags (batch path).
	list, _ := s.ListFeedings(ctx, FeedingFilter{DogID: dog.ID})
	if len(list) != 1 || len(list[0].Tags) != 2 {
		t.Fatalf("ListFeedings tag load: %+v", list)
	}

	if err := s.DetachTag(ctx, feed.ID, cheese.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFeeding(ctx, feed.ID)
	if len(got.Tags) != 1 || got.Tags[0].ID != chicken.ID {
		t.Fatalf("after detach want only chicken, got %+v", got.Tags)
	}

	// SetFeedingTags replaces the whole set.
	if err := s.SetFeedingTags(ctx, feed.ID, []int64{cheese.ID, UnspecifiedTagID}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFeeding(ctx, feed.ID)
	if len(got.Tags) != 2 {
		t.Fatalf("SetFeedingTags should replace to 2 tags, got %+v", got.Tags)
	}
}

func TestTags_ForDogRanking(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	riley := mustDog(t, s, "Riley")
	bard := mustDog(t, s, "Bard")

	cheese, _ := s.CreateTag(ctx, "Brie")
	chicken, _ := s.CreateTag(ctx, "Duck")

	feed := func(dogID int64) int64 {
		f, err := s.CreateFeeding(ctx, domain.Feeding{
			DogID: dogID, TS: time.Now(), Kind: domain.FeedStandard,
			Score: domain.ScoreFull, Source: domain.SourceButton,
		})
		if err != nil {
			t.Fatal(err)
		}
		return f.ID
	}

	// Riley gets chicken twice, cheese once. Bard gets cheese only.
	f1, f2, f3 := feed(riley.ID), feed(riley.ID), feed(riley.ID)
	_ = s.AttachTag(ctx, f1, chicken.ID)
	_ = s.AttachTag(ctx, f2, chicken.ID)
	_ = s.AttachTag(ctx, f3, cheese.ID)
	fb := feed(bard.ID)
	_ = s.AttachTag(ctx, fb, cheese.ID)

	ranked, err := s.TagsForDog(ctx, riley.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) < 2 {
		t.Fatalf("expected ranked tags, got %+v", ranked)
	}
	// chicken (2 uses) must rank above cheese (1 use) for Riley.
	if ranked[0].ID != chicken.ID || ranked[0].Uses != 2 {
		t.Errorf("top ranked for Riley = %+v, want chicken with 2 uses", ranked[0])
	}
	// The Unspecified sentinel must never appear in the ranked body.
	for _, rt := range ranked {
		if rt.ID == UnspecifiedTagID {
			t.Error("ranked tags should exclude the Unspecified sentinel")
		}
	}
}

func mustDog(t *testing.T, s *Store, name string) domain.Dog {
	t.Helper()
	d, err := s.CreateDog(context.Background(), domain.Dog{Name: name, AccentColor: "#A8D8B9"})
	if err != nil {
		t.Fatalf("seed dog: %v", err)
	}
	return d
}

package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

func TestTagsIndex_ShowsCatalog(t *testing.T) {
	srv, _ := newTestServer(t)
	body := readBody(t, do(t, srv, http.MethodGet, "/tags"))
	for _, want := range []string{"Add-in catalog", "Shredded Chicken", "Cheese", "Reserved"} {
		if !strings.Contains(body, want) {
			t.Errorf("tags page missing %q", want)
		}
	}
}

func TestTagCreate_AppearsAndDeduplicates(t *testing.T) {
	srv, _ := newTestServer(t)

	if loc := locationOf(t, postForm(t, srv, "/tags", url.Values{"name": {"bone broth"}})); !strings.Contains(loc, "ok") {
		t.Errorf("create redirect = %q, want an ok flash", loc)
	}
	if !strings.Contains(readBody(t, do(t, srv, http.MethodGet, "/tags")), "Bone Broth") {
		t.Error("created tag should be canonicalized and listed")
	}
	// Duplicate (case-insensitive) is rejected with an error flash.
	if loc := locationOf(t, postForm(t, srv, "/tags", url.Values{"name": {"BONE BROTH"}})); !strings.Contains(loc, "err") {
		t.Errorf("duplicate create redirect = %q, want an err flash", loc)
	}
}

func TestTagRenameAndArchive(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	tag, err := st.CreateTag(ctx, "Sardines")
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(tag.ID)

	// Rename via methodOverride PATCH.
	postForm(t, srv, "/tags/"+id, url.Values{"_method": {"PATCH"}, "name": {"Anchovies"}})
	if !strings.Contains(readBody(t, do(t, srv, http.MethodGet, "/tags")), "Anchovies") {
		t.Error("renamed tag should show new name")
	}
	// Archive via methodOverride DELETE → moves to the Archived section.
	postForm(t, srv, "/tags/"+id, url.Values{"_method": {"DELETE"}})
	got, _ := st.GetTag(ctx, tag.ID)
	if got.ArchivedAt == nil {
		t.Error("tag should be archived after DELETE")
	}
}

func TestFeedingTagAttachDetach_Chips(t *testing.T) {
	_, st := newTestServer(t)
	srv := fakeServer(t, st, time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC), time.UTC)
	dog := seedDog(t, st, "Riley")
	feed, err := st.CreateFeeding(context.Background(), domain.Feeding{
		DogID: dog.ID, TS: time.Now(), Kind: domain.FeedStandard,
		Score: domain.ScoreFull, Source: domain.SourceWeb,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := itoa(feed.ID)

	// Attach by name (created on the fly).
	body := readBody(t, hx(t, srv, http.MethodPost, "/feedings/"+id+"/tags", url.Values{"name": {"Shredded Chicken"}}))
	if !strings.Contains(body, "Shredded Chicken") || !strings.Contains(body, "tags-feeding-"+id) {
		t.Errorf("attach should return the chip area with the new tag; got %s", body)
	}
	tags, _ := st.TagsForFeeding(context.Background(), feed.ID)
	if len(tags) != 1 {
		t.Fatalf("want 1 attached tag, got %d", len(tags))
	}

	// Detach it.
	body = readBody(t, hx(t, srv, http.MethodDelete, "/feedings/"+id+"/tags/"+itoa(tags[0].ID), nil))
	if strings.Contains(body, "Shredded Chicken") {
		t.Errorf("detached tag should be gone; got %s", body)
	}
	tags, _ = st.TagsForFeeding(context.Background(), feed.ID)
	if len(tags) != 0 {
		t.Errorf("want 0 tags after detach, got %d", len(tags))
	}
}

func TestFeedingTagNaming_ClearsUnspecifiedSentinel(t *testing.T) {
	_, st := newTestServer(t)
	srv := fakeServer(t, st, time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC), time.UTC)
	ctx := context.Background()
	dog := seedDog(t, st, "Riley")
	feed, err := st.CreateFeeding(ctx, domain.Feeding{
		DogID: dog.ID, TS: time.Now(), Kind: domain.FeedStandard,
		Score: domain.ScoreFull, Source: domain.SourceButton,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a device "Other" selection.
	if err := st.AttachTag(ctx, feed.ID, store.UnspecifiedTagID); err != nil {
		t.Fatal(err)
	}
	id := itoa(feed.ID)

	// The page should flag it as needing a name.
	if !strings.Contains(readBody(t, do(t, srv, http.MethodGet, "/feedings")), "Needs a name") {
		t.Error("an Unspecified-tagged feeding should show the 'needs a name' affordance")
	}

	// Naming the add-in attaches the real tag and clears the sentinel.
	hx(t, srv, http.MethodPost, "/feedings/"+id+"/tags", url.Values{"name": {"Pumpkin"}})
	tags, _ := st.TagsForFeeding(ctx, feed.ID)
	if len(tags) != 1 || tags[0].IsUnspecified {
		t.Fatalf("naming should leave exactly the named tag, got %+v", tags)
	}
	if tags[0].Name != "Pumpkin" {
		t.Errorf("named tag = %q, want Pumpkin", tags[0].Name)
	}
}

func TestFeedingsIndex_RendersTagSuggestionDatalists(t *testing.T) {
	_, st := newTestServer(t)
	srv := fakeServer(t, st, time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC), time.UTC)
	dog := seedDog(t, st, "Riley")
	body := readBody(t, do(t, srv, http.MethodGet, "/feedings"))
	if !strings.Contains(body, `id="tags-dog-`+itoa(dog.ID)) {
		t.Error("feedings page should render a per-dog add-in suggestion datalist")
	}
	// Seeded catalog names should be offered as suggestions.
	if !strings.Contains(body, "Shredded Chicken") {
		t.Error("datalist should include seeded add-in names")
	}
}

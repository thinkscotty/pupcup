package seed

import "testing"

func TestDogs_ParsesAndOrders(t *testing.T) {
	dogs, err := Dogs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Riley", "Bentley", "Bard"}
	if len(dogs) != len(want) {
		t.Fatalf("got %d dogs, want %d", len(dogs), len(want))
	}
	for i, name := range want {
		if dogs[i].Name != name {
			t.Errorf("dog[%d] = %q, want %q (sort order)", i, dogs[i].Name, name)
		}
		if dogs[i].SortOrder != i {
			t.Errorf("dog[%d] %q sort_order = %d, want %d", i, dogs[i].Name, dogs[i].SortOrder, i)
		}
		if dogs[i].AccentColor == "" {
			t.Errorf("dog[%d] %q missing accent color", i, dogs[i].Name)
		}
	}
}

func TestFeedTags_Parses(t *testing.T) {
	tags, err := FeedTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 8 {
		t.Fatalf("got %d feed tags, want 8: %v", len(tags), tags)
	}
	if tags[0] != "Shredded Chicken" {
		t.Errorf("tags[0] = %q, want %q", tags[0], "Shredded Chicken")
	}
}

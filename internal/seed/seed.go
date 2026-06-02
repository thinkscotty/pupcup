// Package seed holds the first-boot seed data (the household's dogs and the
// starter add-in tag catalog), authored in seed_data.yaml and embedded into
// the binary.
//
// Dogs are applied idempotently by the daemon on first boot (only when the
// dogs table is empty) rather than via a schema migration: the migration path
// runs in every unit test against a fresh in-memory DB, and several state/store
// tests assume an empty dogs table. Seeding at the app layer keeps those tests
// clean while still giving a freshly-deployed device its dogs on day one.
//
// The starter feed_tags are seeded via the add-in SQL migration (build plan
// milestone 10.5); FeedTags exposes the same list so that migration and its
// tests can derive from this single source of truth.
package seed

import (
	_ "embed"
	"fmt"
	"sort"

	"github.com/scottyturner/pupcup/internal/domain"

	"gopkg.in/yaml.v3"
)

//go:embed seed_data.yaml
var raw []byte

// file mirrors the YAML shape of seed_data.yaml.
type file struct {
	Dogs []struct {
		Name        string `yaml:"name"`
		AccentColor string `yaml:"accent_color"`
		SortOrder   int    `yaml:"sort_order"`
	} `yaml:"dogs"`
	FeedTags []string `yaml:"feed_tags"`
}

func parse() (file, error) {
	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return file{}, fmt.Errorf("seed: parse seed_data.yaml: %w", err)
	}
	return f, nil
}

// Dogs returns the seed dogs ordered by sort_order. Each is validated; IDs are
// left zero for the store to assign.
func Dogs() ([]domain.Dog, error) {
	f, err := parse()
	if err != nil {
		return nil, err
	}
	dogs := make([]domain.Dog, 0, len(f.Dogs))
	for _, d := range f.Dogs {
		dog := domain.Dog{Name: d.Name, AccentColor: d.AccentColor, SortOrder: d.SortOrder}
		if err := dog.Validate(); err != nil {
			return nil, fmt.Errorf("seed: dog %q: %w", d.Name, err)
		}
		dogs = append(dogs, dog)
	}
	sort.SliceStable(dogs, func(i, j int) bool { return dogs[i].SortOrder < dogs[j].SortOrder })
	return dogs, nil
}

// FeedTags returns the starter add-in tag names as authored (canonicalization
// to Title Case and case-insensitive dedup happen in the add-in migration /
// tag store methods, milestone 10.5).
func FeedTags() ([]string, error) {
	f, err := parse()
	if err != nil {
		return nil, err
	}
	return f.FeedTags, nil
}

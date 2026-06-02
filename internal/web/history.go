package web

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

// historyLimit bounds how many feedings/snacks the timeline pulls per kind
// before merging. Illness/stress events are few and pulled unbounded.
const historyLimit = 500

// entry-type identifiers used both as the `type` query-param values and as the
// historyItem.Type discriminator the row template branches on.
const (
	histFeeding = "feeding"
	histSnack   = "snack"
	histIllness = "illness"
	histStress  = "stress"
)

// ----------------------------- view models ----------------------------------

type historyData struct {
	baseData
	Dogs    []dogOption
	Items   []historyItem
	Filter  historyFilter
	Count   int
	AnyDogs bool
}

// historyFilter holds the active filter as submitted, so the form can echo the
// user's selection back. DogID 0 = all dogs; Type "" = all entry kinds; From/To
// are the raw <input type="date"> strings (empty = unbounded).
type historyFilter struct {
	DogID int64
	Type  string
	From  string
	To    string
}

// historyItem is one row in the unified timeline. Type selects how the row is
// rendered. When is the sort key (a feeding/snack instant, or an event's start
// date). For events End nil = ongoing; Instant distinguishes a timed entry
// (feeding/snack, shown with a clock) from a calendar-date event.
type historyItem struct {
	Type      string
	ID        int64
	When      time.Time
	DogName   string
	Accent    string
	Household bool
	Instant   bool // true = feeding/snack (time-of-day); false = illness/stress (date)
	Score     domain.Score
	Kind      domain.FeedKind
	EventKind string // stress kind label
	Start     time.Time
	End       *time.Time
	Detail    string // feeding/snack specifics or event notes
}

// CatLabel is the human category shown as the leading tag on every row.
func (h historyItem) CatLabel() string {
	switch h.Type {
	case histFeeding:
		return "Meal"
	case histSnack:
		return "Snack"
	case histIllness:
		return "Illness"
	case histStress:
		return "Stress"
	default:
		return h.Type
	}
}

// DotClass color-codes the leading dot, matching the per-kind pages (meals by
// score, illness red, snacks/stress neutral).
func (h historyItem) DotClass() string {
	switch h.Type {
	case histFeeding:
		return scoreClass(h.Score)
	case histIllness:
		return "score-none"
	default:
		return ""
	}
}

// ----------------------------- history page ---------------------------------

// handleHistory renders the unified, filterable timeline of every recorded
// activity — meals, snacks, illness, and stress — merged newest-first. It is a
// read-only view (the per-kind pages handle edits); filtering is plain GET so
// it works without JavaScript and is bookmarkable.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filter := parseHistoryFilter(r)

	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		s.serverError(w, "history", err)
		return
	}
	data := historyData{
		baseData: s.base("history"),
		Filter:   filter,
		AnyDogs:  len(dogs) > 0,
	}
	for _, d := range dogs {
		data.Dogs = append(data.Dogs, dogOption{ID: d.ID, Name: d.Name, Accent: d.AccentColor})
	}

	items, err := s.historyItems(ctx, filter)
	if err != nil {
		s.serverError(w, "history", err)
		return
	}
	data.Items = items
	data.Count = len(items)

	if err := s.tmpl.render(w, http.StatusOK, "history", data); err != nil {
		s.serverError(w, "history", err)
	}
}

// parseHistoryFilter reads the dog / type / date-range query params. Unknown
// type values and unparseable dates are treated as "no filter".
func parseHistoryFilter(r *http.Request) historyFilter {
	f := historyFilter{From: strings.TrimSpace(r.URL.Query().Get("from")), To: strings.TrimSpace(r.URL.Query().Get("to"))}
	if id, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("dog")), 10, 64); err == nil {
		f.DogID = id
	}
	switch t := strings.TrimSpace(r.URL.Query().Get("type")); t {
	case histFeeding, histSnack, histIllness, histStress:
		f.Type = t
	}
	return f
}

// historyItems gathers and merges the filtered entries into one newest-first
// slice. Feedings/snacks are date-range-filtered in the store; illness/stress
// (few rows) are pulled per-dog and overlap-filtered in memory.
func (s *Server) historyItems(ctx context.Context, f historyFilter) ([]historyItem, error) {
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]domain.Dog, len(dogs))
	for _, d := range dogs {
		byID[d.ID] = d
	}

	since, until := s.feedBounds(f.From, f.To)
	evtFrom, hasFrom := parseDateOnly(f.From)
	evtTo, hasTo := parseDateOnly(f.To)

	var items []historyItem

	if f.Type == "" || f.Type == histFeeding {
		feeds, err := s.store.ListFeedings(ctx, store.FeedingFilter{DogID: f.DogID, Since: since, Until: until, Limit: historyLimit})
		if err != nil {
			return nil, err
		}
		for _, fd := range feeds {
			d := byID[fd.DogID]
			items = append(items, historyItem{
				Type: histFeeding, ID: fd.ID, When: fd.TS, DogName: d.Name, Accent: d.AccentColor,
				Instant: true, Score: fd.Score, Kind: fd.Kind, Detail: fd.Specifics,
			})
		}
	}

	if f.Type == "" || f.Type == histSnack {
		snacks, err := s.store.ListSnacks(ctx, store.SnackFilter{DogID: f.DogID, Since: since, Until: until, Limit: historyLimit})
		if err != nil {
			return nil, err
		}
		for _, sn := range snacks {
			d := byID[sn.DogID]
			items = append(items, historyItem{
				Type: histSnack, ID: sn.ID, When: sn.TS, DogName: d.Name, Accent: d.AccentColor,
				Instant: true, Detail: sn.Specifics,
			})
		}
	}

	if f.Type == "" || f.Type == histIllness {
		events, err := s.store.ListIllness(ctx, f.DogID)
		if err != nil {
			return nil, err
		}
		for _, e := range events {
			if !eventInRange(e.Start, e.End, evtFrom, hasFrom, evtTo, hasTo) {
				continue
			}
			d := byID[e.DogID]
			items = append(items, historyItem{
				Type: histIllness, ID: e.ID, When: e.Start, DogName: d.Name, Accent: d.AccentColor,
				Start: e.Start, End: e.End, Detail: e.Notes,
			})
		}
	}

	if f.Type == "" || f.Type == histStress {
		events, err := s.store.ListStress(ctx, f.DogID)
		if err != nil {
			return nil, err
		}
		for _, e := range events {
			if !eventInRange(e.Start, e.End, evtFrom, hasFrom, evtTo, hasTo) {
				continue
			}
			item := historyItem{
				Type: histStress, ID: e.ID, When: e.Start, Start: e.Start, End: e.End,
				EventKind: e.Kind, Detail: e.Notes,
			}
			if e.DogID == nil {
				item.Household = true
				item.DogName = "Whole household"
			} else {
				d := byID[*e.DogID]
				item.DogName = d.Name
				item.Accent = d.AccentColor
			}
			items = append(items, item)
		}
	}

	// Newest first; tie-break by type then id for a stable order.
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].When.Equal(items[j].When) {
			return items[i].When.After(items[j].When)
		}
		if items[i].Type != items[j].Type {
			return items[i].Type < items[j].Type
		}
		return items[i].ID > items[j].ID
	})
	return items, nil
}

// feedBounds converts the From/To calendar dates into the UTC instant bounds
// ListFeedings/ListSnacks expect: Since is the start of the From day and Until
// is the start of the day after To (the filter is ts >= Since AND ts < Until),
// both interpreted in the household location. A blank/invalid bound is zero
// (the store treats a zero time as unbounded).
func (s *Server) feedBounds(from, to string) (since, until time.Time) {
	if t, ok := parseDateOnly(from); ok {
		since = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, s.loc).UTC()
	}
	if t, ok := parseDateOnly(to); ok {
		until = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, s.loc).AddDate(0, 0, 1).UTC()
	}
	return since, until
}

// eventInRange reports whether a [start, end] event (end nil = ongoing/open)
// overlaps the [from, to] window. An absent from/to bound is treated as open on
// that side.
func eventInRange(start time.Time, end *time.Time, from time.Time, hasFrom bool, to time.Time, hasTo bool) bool {
	if hasTo && start.After(to) {
		return false // event begins after the window closes
	}
	if hasFrom && end != nil && end.Before(from) {
		return false // event ended before the window opens
	}
	return true
}

// parseDateOnly parses a calendar date (the value an <input type="date">
// submits) as a UTC date, matching how illness/stress dates are stored. ok is
// false for a blank or malformed value.
func parseDateOnly(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(inputDateLayout, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

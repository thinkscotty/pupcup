package web

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

// recentLimit bounds how many of each kind (feedings, snacks) the /feedings
// page pulls before merging — the unified, filterable timeline is milestone 12.
const recentLimit = 50

// inputTimeLayout is the format an <input type="datetime-local"> submits
// (browser local wall-clock, no zone). We interpret it in the household loc.
const inputTimeLayout = "2006-01-02T15:04"

// ----------------------------- view models ----------------------------------

type feedingsData struct {
	baseData
	Dogs        []dogOption
	Entries     []entryView
	DogTagLists []dogTagList // per-dog ranked add-in names → type-ahead datalists
	NowInput    string       // default value for the add-form datetime-local picker
	Flash       *flash
	AnyDogs     bool
}

// dogOption is a dog as an <option> in the add-form selects.
type dogOption struct {
	ID     int64
	Name   string
	Accent string
}

// entryView is one row in the merged recent-activity list: a meal feeding or a
// snack. IsSnack selects which fields and which edit form the row uses.
type entryView struct {
	ID        int64
	IsSnack   bool
	DogID     int64
	DogName   string
	Accent    string
	TS        time.Time
	Score     domain.Score    // meals only
	Kind      domain.FeedKind // meals only
	Specifics string
	Edited    bool
	// Unverified: a meal the device recorded before its clock synced; the row
	// shows a badge prompting the household to confirm the time (meals only).
	Unverified bool
	Source     domain.Source
	// TagArea carries the add-in chips + add control for meal rows; nil for
	// snacks (which can't be tagged).
	TagArea *feedingTags
}

// ----------------------------- feedings page --------------------------------

// handleFeedingsIndex renders the add forms and the merged recent-activity
// list. It's the household's "log a meal / fix a mistake" surface; the
// dashboard is the at-a-glance view.
func (s *Server) handleFeedingsIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		s.serverError(w, "feedings", err)
		return
	}
	data := feedingsData{
		baseData: s.base("feedings"),
		Flash:    readFlash(r),
		AnyDogs:  len(dogs) > 0,
		NowInput: s.clk.Now().In(s.loc).Format(inputTimeLayout),
	}
	for _, d := range dogs {
		data.Dogs = append(data.Dogs, dogOption{ID: d.ID, Name: d.Name, Accent: d.AccentColor})
	}
	data.DogTagLists = s.dogTagLists(ctx, dogs)
	data.Entries, err = s.recentEntries(ctx)
	if err != nil {
		s.serverError(w, "feedings", err)
		return
	}
	if err := s.tmpl.render(w, http.StatusOK, "feedings", data); err != nil {
		s.serverError(w, "feedings", err)
	}
}

// recentEntries merges the most recent feedings and snacks into one
// reverse-chronological slice, resolving dog names/accents from one ListDogs.
func (s *Server) recentEntries(ctx context.Context) ([]entryView, error) {
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]domain.Dog, len(dogs))
	for _, d := range dogs {
		byID[d.ID] = d
	}
	feeds, err := s.store.ListFeedings(ctx, store.FeedingFilter{Limit: recentLimit})
	if err != nil {
		return nil, err
	}
	snacks, err := s.store.ListSnacks(ctx, store.SnackFilter{Limit: recentLimit})
	if err != nil {
		return nil, err
	}
	out := make([]entryView, 0, len(feeds)+len(snacks))
	for _, f := range feeds {
		d := byID[f.DogID]
		ta := feedingTagsView(f)
		out = append(out, entryView{
			ID: f.ID, DogID: f.DogID, DogName: d.Name, Accent: d.AccentColor,
			TS: f.TS, Score: f.Score, Kind: f.Kind, Specifics: f.Specifics,
			Edited: f.EditedAt != nil, Unverified: f.TimeUnverified, Source: f.Source, TagArea: &ta,
		})
	}
	for _, sn := range snacks {
		d := byID[sn.DogID]
		out = append(out, entryView{
			ID: sn.ID, IsSnack: true, DogID: sn.DogID, DogName: d.Name, Accent: d.AccentColor,
			TS: sn.TS, Specifics: sn.Specifics, Edited: sn.EditedAt != nil, Source: sn.Source,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.After(out[j].TS) })
	if len(out) > recentLimit {
		out = out[:recentLimit]
	}
	return out, nil
}

// ----------------------------- feeding create -------------------------------

// handleFeedingCreate records a meal. The dashboard quick-add buttons pass
// return=card and get the refreshed dog status card; the /feedings add form
// gets the new row prepended (plus an OOB removal of the empty placeholder).
// Without JS (no HX-Request) it falls back to a Post/Redirect/Get.
func (s *Server) handleFeedingCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dogID, err := parseDogID(r)
	if err != nil {
		s.createError(w, r, "#feed-msg", err.Error())
		return
	}
	ts, score, kind, specifics, err := s.parseFeedingFields(r)
	if err != nil {
		s.createError(w, r, "#feed-msg", err.Error())
		return
	}
	if _, gerr := s.store.GetDog(ctx, dogID); gerr != nil {
		s.createError(w, r, "#feed-msg", "pick a dog")
		return
	}
	fd, err := s.store.CreateFeeding(ctx, domain.Feeding{
		DogID: dogID, TS: ts, Kind: kind, Score: score,
		Specifics: specifics, Source: domain.SourceWeb,
	})
	if err != nil {
		s.log.Error("create feeding", "err", err)
		s.createError(w, r, "#feed-msg", "couldn't record that meal")
		return
	}

	if r.FormValue("return") == "card" {
		s.renderDogCard(w, r, dogID)
		return
	}
	if !isHTMX(r) {
		s.flashRedirect(w, r, "/feedings", "ok", "recorded a meal for "+s.dogName(ctx, dogID))
		return
	}
	s.renderNewEntry(w, s.feedingEntry(ctx, fd))
}

// ----------------------------- feeding edit / update ------------------------

func (s *Server) handleFeedingEditForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	fd, err := s.store.GetFeeding(ctx, id)
	if err != nil || fd.DeletedAt != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.tmpl.fragment(w, http.StatusOK, "feedings", "feeding_edit", s.feedingEntry(ctx, fd)); err != nil {
		s.serverError(w, "feeding edit", err)
	}
}

// handleFeedingRow returns the read-only row fragment — used by the edit form's
// Cancel button to restore the row without a full reload.
func (s *Server) handleFeedingRow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	fd, err := s.store.GetFeeding(ctx, id)
	if err != nil || fd.DeletedAt != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.tmpl.fragment(w, http.StatusOK, "feedings", "entry_row", s.feedingEntry(ctx, fd)); err != nil {
		s.serverError(w, "feeding row", err)
	}
}

func (s *Server) handleFeedingUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	fd, err := s.store.GetFeeding(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.handleNotFound(w, r)
			return
		}
		s.serverError(w, "feeding update", err)
		return
	}
	ts, score, kind, specifics, perr := s.parseFeedingFields(r)
	if perr != nil {
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-feeding-%d", id), "innerHTML", perr.Error())
		return
	}
	fd.TS, fd.Score, fd.Kind, fd.Specifics = ts, score, kind, specifics
	if err := s.store.UpdateFeeding(ctx, fd); err != nil {
		s.log.Error("update feeding", "id", id, "err", err)
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-feeding-%d", id), "innerHTML", "couldn't save changes")
		return
	}
	fd, _ = s.store.GetFeeding(ctx, id)
	if err := s.tmpl.fragment(w, http.StatusOK, "feedings", "entry_row", s.feedingEntry(ctx, fd)); err != nil {
		s.serverError(w, "feeding update", err)
	}
}

func (s *Server) handleFeedingDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.store.SoftDeleteFeeding(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("delete feeding", "id", id, "err", err)
		s.htmxBanner(w, fmt.Sprintf("#feeding-%d", id), "afterbegin", "couldn't delete that")
		return
	}
	// Empty body + 200: htmx swaps the row out (hx-swap="outerHTML").
	w.WriteHeader(http.StatusOK)
}

// ----------------------------- snack create / edit --------------------------

func (s *Server) handleSnackCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dogID, err := parseDogID(r)
	if err != nil {
		s.createError(w, r, "#snack-msg", err.Error())
		return
	}
	ts, specifics, err := s.parseSnackFields(r)
	if err != nil {
		s.createError(w, r, "#snack-msg", err.Error())
		return
	}
	if _, gerr := s.store.GetDog(ctx, dogID); gerr != nil {
		s.createError(w, r, "#snack-msg", "pick a dog")
		return
	}
	sn, err := s.store.CreateSnack(ctx, domain.Snack{
		DogID: dogID, TS: ts, Specifics: specifics, Source: domain.SourceWeb,
	})
	if err != nil {
		s.log.Error("create snack", "err", err)
		s.createError(w, r, "#snack-msg", "couldn't record that snack")
		return
	}
	if !isHTMX(r) {
		s.flashRedirect(w, r, "/feedings", "ok", "recorded a snack for "+s.dogName(ctx, dogID))
		return
	}
	s.renderNewEntry(w, s.snackEntry(ctx, sn))
}

func (s *Server) handleSnackEditForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	sn, err := s.store.GetSnack(ctx, id)
	if err != nil || sn.DeletedAt != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.tmpl.fragment(w, http.StatusOK, "feedings", "snack_edit", s.snackEntry(ctx, sn)); err != nil {
		s.serverError(w, "snack edit", err)
	}
}

func (s *Server) handleSnackRow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	sn, err := s.store.GetSnack(ctx, id)
	if err != nil || sn.DeletedAt != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.tmpl.fragment(w, http.StatusOK, "feedings", "entry_row", s.snackEntry(ctx, sn)); err != nil {
		s.serverError(w, "snack row", err)
	}
}

func (s *Server) handleSnackUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	sn, err := s.store.GetSnack(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.handleNotFound(w, r)
			return
		}
		s.serverError(w, "snack update", err)
		return
	}
	ts, specifics, perr := s.parseSnackFields(r)
	if perr != nil {
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-snack-%d", id), "innerHTML", perr.Error())
		return
	}
	sn.TS, sn.Specifics = ts, specifics
	if err := s.store.UpdateSnack(ctx, sn); err != nil {
		s.log.Error("update snack", "id", id, "err", err)
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-snack-%d", id), "innerHTML", "couldn't save changes")
		return
	}
	sn, _ = s.store.GetSnack(ctx, id)
	if err := s.tmpl.fragment(w, http.StatusOK, "feedings", "entry_row", s.snackEntry(ctx, sn)); err != nil {
		s.serverError(w, "snack update", err)
	}
}

func (s *Server) handleSnackDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.store.SoftDeleteSnack(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("delete snack", "id", id, "err", err)
		s.htmxBanner(w, fmt.Sprintf("#snack-%d", id), "afterbegin", "couldn't delete that")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ----------------------------- form parsing ---------------------------------

// parseDogID reads the dog_id field (create only — the dog isn't editable).
func parseDogID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("dog_id")), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("pick a dog")
	}
	return id, nil
}

// parseFeedingFields reads the editable meal fields (ts, score, kind,
// specifics), shared by create and update; dog_id is parsed separately.
func (s *Server) parseFeedingFields(r *http.Request) (ts time.Time, score domain.Score, kind domain.FeedKind, specifics string, err error) {
	score = domain.Score(strings.TrimSpace(r.FormValue("score")))
	if !score.Valid() {
		return time.Time{}, "", "", "", fmt.Errorf("pick how well they ate")
	}
	kind = domain.FeedKind(strings.TrimSpace(r.FormValue("kind")))
	if kind == "" {
		kind = domain.FeedStandard
	}
	if !kind.Valid() {
		return time.Time{}, "", "", "", fmt.Errorf("invalid meal kind")
	}
	ts, err = s.parseInputTime(r.FormValue("ts"))
	if err != nil {
		return time.Time{}, "", "", "", err
	}
	specifics = strings.TrimSpace(r.FormValue("specifics"))
	return ts, score, kind, specifics, nil
}

// parseSnackFields reads the editable snack fields (ts, specifics).
func (s *Server) parseSnackFields(r *http.Request) (ts time.Time, specifics string, err error) {
	ts, err = s.parseInputTime(r.FormValue("ts"))
	if err != nil {
		return time.Time{}, "", err
	}
	specifics = strings.TrimSpace(r.FormValue("specifics"))
	return ts, specifics, nil
}

// parseInputTime reads a datetime-local value as household-local wall-clock and
// returns it in UTC. An empty value means "now" (the common quick-add case).
func (s *Server) parseInputTime(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return s.clk.Now().UTC(), nil
	}
	// Browsers may include seconds when the user picks them.
	for _, layout := range []string{inputTimeLayout, "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, v, s.loc); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("that date/time wasn't understood")
}

// ----------------------------- fragment helpers -----------------------------

func (s *Server) feedingEntry(ctx context.Context, f domain.Feeding) entryView {
	d, _ := s.store.GetDog(ctx, f.DogID)
	ta := feedingTagsView(f)
	return entryView{
		ID: f.ID, DogID: f.DogID, DogName: d.Name, Accent: d.AccentColor,
		TS: f.TS, Score: f.Score, Kind: f.Kind, Specifics: f.Specifics,
		Edited: f.EditedAt != nil, Unverified: f.TimeUnverified, Source: f.Source, TagArea: &ta,
	}
}

func (s *Server) snackEntry(ctx context.Context, sn domain.Snack) entryView {
	d, _ := s.store.GetDog(ctx, sn.DogID)
	return entryView{
		ID: sn.ID, IsSnack: true, DogID: sn.DogID, DogName: d.Name, Accent: d.AccentColor,
		TS: sn.TS, Specifics: sn.Specifics, Edited: sn.EditedAt != nil, Source: sn.Source,
	}
}

// renderNewEntry prepends a freshly-created entry to the list and emits an OOB
// swap that removes the "nothing yet" placeholder if it's present.
func (s *Server) renderNewEntry(w http.ResponseWriter, e entryView) {
	if err := s.tmpl.fragment(w, http.StatusOK, "feedings", "new_entry", e); err != nil {
		s.serverError(w, "new entry", err)
	}
}

// renderDogCard re-renders one dashboard dog status card (the quick-add target).
func (s *Server) renderDogCard(w http.ResponseWriter, r *http.Request, dogID int64) {
	ctx := r.Context()
	dog, err := s.store.GetDog(ctx, dogID)
	if err != nil {
		s.log.Error("quick-add card", "dog", dogID, "err", err)
		w.Header().Set("HX-Reswap", "none")
		w.WriteHeader(http.StatusOK)
		return
	}
	startUTC := startOfLocalDay(s.clk.Now().In(s.loc)).UTC()
	if err := s.tmpl.fragment(w, http.StatusOK, "dashboard", "dog_status_card", s.dogStatusFor(ctx, dog, startUTC)); err != nil {
		s.serverError(w, "dog card", err)
	}
}

func (s *Server) dogName(ctx context.Context, id int64) string {
	if d, err := s.store.GetDog(ctx, id); err == nil {
		return d.Name
	}
	return "that dog"
}

// createError reports an add-form failure: as an htmx retargeted banner when
// scripted, or a PRG flash when the form was submitted without JS.
func (s *Server) createError(w http.ResponseWriter, r *http.Request, target, msg string) {
	if isHTMX(r) {
		s.htmxBanner(w, target, "innerHTML", msg)
		return
	}
	s.flashRedirect(w, r, "/feedings", "err", msg)
}

// htmxBanner retargets the swap at a message container and writes an error
// banner, so a validation failure never injects bad content into the list or
// destroys the row/form being edited. reswap picks the swap style (innerHTML
// for a dedicated message slot, afterbegin to prepend into a row).
func (s *Server) htmxBanner(w http.ResponseWriter, target, reswap, msg string) {
	w.Header().Set("HX-Retarget", target)
	w.Header().Set("HX-Reswap", reswap)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<div class="flash flash-err">%s</div>`, html.EscapeString(msg))
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

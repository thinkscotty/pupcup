package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

// inputDateLayout is the value an <input type="date"> submits. Illness and
// stress events are calendar dates (no time-of-day), so — unlike feedings —
// they are parsed and stored in UTC without any timezone shift, which keeps the
// calendar day stable regardless of the household location (see parseEventDate).
const inputDateLayout = "2006-01-02"

// ----------------------------- illness --------------------------------------

type illnessData struct {
	baseData
	Dogs    []dogOption
	Events  []illnessView
	Today   string // default value for the add-form date pickers
	Flash   *flash
	AnyDogs bool
}

// illnessView is one illness event in the list. End nil = ongoing. Today is
// carried so the "set end" inline form on an ongoing row can default to today
// even when the row is re-rendered standalone as an HTMX fragment.
type illnessView struct {
	ID      int64
	DogID   int64
	DogName string
	Accent  string
	Start   time.Time
	End     *time.Time
	Notes   string
	Today   string
}

func (s *Server) handleIllnessIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		s.serverError(w, "illness", err)
		return
	}
	data := illnessData{
		baseData: s.base("illness"),
		Flash:    readFlash(r),
		AnyDogs:  len(dogs) > 0,
		Today:    s.todayInput(),
	}
	for _, d := range dogs {
		data.Dogs = append(data.Dogs, dogOption{ID: d.ID, Name: d.Name, Accent: d.AccentColor})
	}
	data.Events, err = s.illnessEvents(ctx)
	if err != nil {
		s.serverError(w, "illness", err)
		return
	}
	if err := s.tmpl.render(w, http.StatusOK, "illness", data); err != nil {
		s.serverError(w, "illness", err)
	}
}

func (s *Server) illnessEvents(ctx context.Context) ([]illnessView, error) {
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]domain.Dog, len(dogs))
	for _, d := range dogs {
		byID[d.ID] = d
	}
	events, err := s.store.ListIllness(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make([]illnessView, 0, len(events))
	for _, e := range events {
		out = append(out, s.illnessView(byID[e.DogID], e))
	}
	return out, nil
}

func (s *Server) illnessView(d domain.Dog, e domain.IllnessEvent) illnessView {
	return illnessView{
		ID: e.ID, DogID: e.DogID, DogName: d.Name, Accent: d.AccentColor,
		Start: e.Start, End: e.End, Notes: e.Notes, Today: s.todayInput(),
	}
}

func (s *Server) handleIllnessCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dogID, err := parseDogID(r)
	if err != nil {
		s.eventCreateError(w, r, "/illness", "#illness-msg", err.Error())
		return
	}
	start, end, notes, err := s.parseIllnessFields(r)
	if err != nil {
		s.eventCreateError(w, r, "/illness", "#illness-msg", err.Error())
		return
	}
	d, gerr := s.store.GetDog(ctx, dogID)
	if gerr != nil {
		s.eventCreateError(w, r, "/illness", "#illness-msg", "pick a dog")
		return
	}
	ev, err := s.store.CreateIllness(ctx, domain.IllnessEvent{
		DogID: dogID, Start: start, End: end, Notes: notes,
	})
	if err != nil {
		s.log.Error("create illness", "err", err)
		s.eventCreateError(w, r, "/illness", "#illness-msg", "couldn't save that")
		return
	}
	if !isHTMX(r) {
		s.flashRedirect(w, r, "/illness", "ok", "logged an illness for "+d.Name)
		return
	}
	if err := s.tmpl.fragment(w, http.StatusOK, "illness", "illness_new", s.illnessView(d, ev)); err != nil {
		s.serverError(w, "illness new", err)
	}
}

func (s *Server) handleIllnessRow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	ev, err := s.store.GetIllness(ctx, id)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	d, _ := s.store.GetDog(ctx, ev.DogID)
	if err := s.tmpl.fragment(w, http.StatusOK, "illness", "illness_row", s.illnessView(d, ev)); err != nil {
		s.serverError(w, "illness row", err)
	}
}

func (s *Server) handleIllnessEditForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	ev, err := s.store.GetIllness(ctx, id)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	d, _ := s.store.GetDog(ctx, ev.DogID)
	if err := s.tmpl.fragment(w, http.StatusOK, "illness", "illness_edit", s.illnessView(d, ev)); err != nil {
		s.serverError(w, "illness edit", err)
	}
}

// handleIllnessUpdate backs both the full edit form and the ongoing-row "set
// end" quick action (which posts the unchanged start/notes as hidden fields).
func (s *Server) handleIllnessUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	ev, err := s.store.GetIllness(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.handleNotFound(w, r)
			return
		}
		s.serverError(w, "illness update", err)
		return
	}
	start, end, notes, perr := s.parseIllnessFields(r)
	if perr != nil {
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-illness-%d", id), "innerHTML", perr.Error())
		return
	}
	ev.Start, ev.End, ev.Notes = start, end, notes
	if err := s.store.UpdateIllness(ctx, ev); err != nil {
		s.log.Error("update illness", "id", id, "err", err)
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-illness-%d", id), "innerHTML", "couldn't save changes")
		return
	}
	ev, _ = s.store.GetIllness(ctx, id)
	d, _ := s.store.GetDog(ctx, ev.DogID)
	if err := s.tmpl.fragment(w, http.StatusOK, "illness", "illness_row", s.illnessView(d, ev)); err != nil {
		s.serverError(w, "illness update", err)
	}
}

func (s *Server) handleIllnessDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.store.DeleteIllness(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("delete illness", "id", id, "err", err)
		s.htmxBanner(w, fmt.Sprintf("#illness-%d", id), "afterbegin", "couldn't delete that")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// parseIllnessFields reads the editable illness fields. End is nil (ongoing) if
// the "ongoing" toggle is set or the end date is blank; otherwise it must be on
// or after the start date.
func (s *Server) parseIllnessFields(r *http.Request) (start time.Time, end *time.Time, notes string, err error) {
	start, err = s.parseEventDate(r.FormValue("start"))
	if err != nil {
		return time.Time{}, nil, "", err
	}
	end, err = s.parseEndDate(r, start)
	if err != nil {
		return time.Time{}, nil, "", err
	}
	notes = strings.TrimSpace(r.FormValue("notes"))
	return start, end, notes, nil
}

// ----------------------------- stress ---------------------------------------

type stressData struct {
	baseData
	Dogs   []dogOption
	Events []stressView
	Today  string
	Flash  *flash
}

// stressView is one stress event. DogID nil = whole household (DogName then
// reads "Whole household").
type stressView struct {
	ID        int64
	DogID     *int64
	DogName   string
	Accent    string
	Household bool
	Start     time.Time
	End       *time.Time
	Kind      string
	Notes     string
	Today     string
}

func (s *Server) handleStressIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		s.serverError(w, "stress", err)
		return
	}
	data := stressData{
		baseData: s.base("stress"),
		Flash:    readFlash(r),
		Today:    s.todayInput(),
	}
	for _, d := range dogs {
		data.Dogs = append(data.Dogs, dogOption{ID: d.ID, Name: d.Name, Accent: d.AccentColor})
	}
	data.Events, err = s.stressEvents(ctx)
	if err != nil {
		s.serverError(w, "stress", err)
		return
	}
	if err := s.tmpl.render(w, http.StatusOK, "stress", data); err != nil {
		s.serverError(w, "stress", err)
	}
}

func (s *Server) stressEvents(ctx context.Context) ([]stressView, error) {
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]domain.Dog, len(dogs))
	for _, d := range dogs {
		byID[d.ID] = d
	}
	events, err := s.store.ListStress(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make([]stressView, 0, len(events))
	for _, e := range events {
		var d domain.Dog
		if e.DogID != nil {
			d = byID[*e.DogID]
		}
		out = append(out, s.stressView(d, e))
	}
	return out, nil
}

func (s *Server) stressView(d domain.Dog, e domain.StressEvent) stressView {
	v := stressView{
		ID: e.ID, DogID: e.DogID, Start: e.Start, End: e.End,
		Kind: e.Kind, Notes: e.Notes, Today: s.todayInput(),
	}
	if e.DogID == nil {
		v.Household = true
		v.DogName = "Whole household"
	} else {
		v.DogName = d.Name
		v.Accent = d.AccentColor
	}
	return v
}

func (s *Server) handleStressCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dogID, derr := s.parseStressDogID(ctx, r)
	if derr != nil {
		s.eventCreateError(w, r, "/stress", "#stress-msg", derr.Error())
		return
	}
	start, end, kind, notes, err := s.parseStressFields(r)
	if err != nil {
		s.eventCreateError(w, r, "/stress", "#stress-msg", err.Error())
		return
	}
	ev, err := s.store.CreateStress(ctx, domain.StressEvent{
		DogID: dogID, Start: start, End: end, Kind: kind, Notes: notes,
	})
	if err != nil {
		s.log.Error("create stress", "err", err)
		s.eventCreateError(w, r, "/stress", "#stress-msg", "couldn't save that")
		return
	}
	var d domain.Dog
	if dogID != nil {
		d, _ = s.store.GetDog(ctx, *dogID)
	}
	if !isHTMX(r) {
		s.flashRedirect(w, r, "/stress", "ok", "logged a stress event")
		return
	}
	if err := s.tmpl.fragment(w, http.StatusOK, "stress", "stress_new", s.stressView(d, ev)); err != nil {
		s.serverError(w, "stress new", err)
	}
}

func (s *Server) handleStressRow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	ev, err := s.store.GetStress(ctx, id)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.tmpl.fragment(w, http.StatusOK, "stress", "stress_row", s.stressViewFor(ctx, ev)); err != nil {
		s.serverError(w, "stress row", err)
	}
}

func (s *Server) handleStressEditForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	ev, err := s.store.GetStress(ctx, id)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	sel := int64(0)
	if ev.DogID != nil {
		sel = *ev.DogID
	}
	data := struct {
		stressView
		Dogs   []dogOption
		SelDog int64
	}{stressView: s.stressViewFor(ctx, ev), Dogs: s.dogOptions(ctx), SelDog: sel}
	if err := s.tmpl.fragment(w, http.StatusOK, "stress", "stress_edit", data); err != nil {
		s.serverError(w, "stress edit", err)
	}
}

func (s *Server) handleStressUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	ev, err := s.store.GetStress(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.handleNotFound(w, r)
			return
		}
		s.serverError(w, "stress update", err)
		return
	}
	dogID, derr := s.parseStressDogID(ctx, r)
	if derr != nil {
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-stress-%d", id), "innerHTML", derr.Error())
		return
	}
	start, end, kind, notes, perr := s.parseStressFields(r)
	if perr != nil {
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-stress-%d", id), "innerHTML", perr.Error())
		return
	}
	ev.DogID, ev.Start, ev.End, ev.Kind, ev.Notes = dogID, start, end, kind, notes
	if err := s.store.UpdateStress(ctx, ev); err != nil {
		s.log.Error("update stress", "id", id, "err", err)
		s.htmxBanner(w, fmt.Sprintf("#edit-msg-stress-%d", id), "innerHTML", "couldn't save changes")
		return
	}
	ev, _ = s.store.GetStress(ctx, id)
	if err := s.tmpl.fragment(w, http.StatusOK, "stress", "stress_row", s.stressViewFor(ctx, ev)); err != nil {
		s.serverError(w, "stress update", err)
	}
}

func (s *Server) handleStressDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.store.DeleteStress(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("delete stress", "id", id, "err", err)
		s.htmxBanner(w, fmt.Sprintf("#stress-%d", id), "afterbegin", "couldn't delete that")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) stressViewFor(ctx context.Context, e domain.StressEvent) stressView {
	var d domain.Dog
	if e.DogID != nil {
		d, _ = s.store.GetDog(ctx, *e.DogID)
	}
	return s.stressView(d, e)
}

// parseStressDogID reads the optional dog selector. Blank/0 = whole household
// (nil); a named dog must exist.
func (s *Server) parseStressDogID(ctx context.Context, r *http.Request) (*int64, error) {
	v := strings.TrimSpace(r.FormValue("dog_id"))
	if v == "" || v == "0" {
		return nil, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("pick a dog or whole household")
	}
	if _, err := s.store.GetDog(ctx, id); err != nil {
		return nil, fmt.Errorf("that dog wasn't found")
	}
	return &id, nil
}

// parseStressFields reads the editable stress fields (dog handled separately).
func (s *Server) parseStressFields(r *http.Request) (start time.Time, end *time.Time, kind, notes string, err error) {
	start, err = s.parseEventDate(r.FormValue("start"))
	if err != nil {
		return time.Time{}, nil, "", "", err
	}
	end, err = s.parseEndDate(r, start)
	if err != nil {
		return time.Time{}, nil, "", "", err
	}
	kind = strings.TrimSpace(r.FormValue("kind"))
	notes = strings.TrimSpace(r.FormValue("notes"))
	return start, end, kind, notes, nil
}

// ----------------------------- shared helpers -------------------------------

// parseEventDate reads a required <input type="date"> value as a calendar date.
// Dates are kept in UTC (not shifted into the household location) so the stored
// day round-trips unchanged on display regardless of timezone.
func (s *Server) parseEventDate(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, fmt.Errorf("pick a start date")
	}
	t, err := time.Parse(inputDateLayout, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("that date wasn't understood")
	}
	return t, nil
}

// parseEndDate resolves the end of an event: nil (ongoing) when the "ongoing"
// toggle is set or the end field is blank, otherwise a date on or after start.
func (s *Server) parseEndDate(r *http.Request, start time.Time) (*time.Time, error) {
	if r.FormValue("ongoing") != "" {
		return nil, nil
	}
	v := strings.TrimSpace(r.FormValue("end"))
	if v == "" {
		return nil, nil
	}
	end, err := time.Parse(inputDateLayout, v)
	if err != nil {
		return nil, fmt.Errorf("that end date wasn't understood")
	}
	if end.Before(start) {
		return nil, fmt.Errorf("the end date can't be before the start date")
	}
	return &end, nil
}

func (s *Server) todayInput() string {
	return s.clk.Now().In(s.loc).Format(inputDateLayout)
}

func (s *Server) dogOptions(ctx context.Context) []dogOption {
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		s.log.Error("dog options", "err", err)
		return nil
	}
	out := make([]dogOption, 0, len(dogs))
	for _, d := range dogs {
		out = append(out, dogOption{ID: d.ID, Name: d.Name, Accent: d.AccentColor})
	}
	return out
}

// eventCreateError reports an add-form failure: an htmx retargeted banner when
// scripted, or a PRG flash when the form was submitted without JS.
func (s *Server) eventCreateError(w http.ResponseWriter, r *http.Request, redirect, target, msg string) {
	if isHTMX(r) {
		s.htmxBanner(w, target, "innerHTML", msg)
		return
	}
	s.flashRedirect(w, r, redirect, "err", msg)
}

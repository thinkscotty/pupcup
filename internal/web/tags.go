package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

const maxTagNameLen = 40

// ----------------------------- view models ----------------------------------

type tagsData struct {
	baseData
	Live     []tagView
	Archived []tagView
	Flash    *flash
}

type tagView struct {
	ID            int64
	Name          string
	IsUnspecified bool
}

// tagChip is one attached add-in on a feeding row.
type tagChip struct {
	ID            int64
	Name          string
	IsUnspecified bool
}

// feedingTags is the swap target rendered under a meal row: the attached add-in
// chips plus the add control (a datalist-backed name input). Rendered both
// inline in entry_row and standalone by the attach/detach handlers.
type feedingTags struct {
	FeedingID      int64
	DogID          int64
	Tags           []tagChip
	HasUnspecified bool // carries the reserved sentinel → show the "name it" affordance
}

// dogTagList is one dog's ranked add-in names, emitted as a <datalist> the
// feeding rows reference for type-ahead suggestions.
type dogTagList struct {
	DogID int64
	Names []string
}

// ----------------------------- catalog page ---------------------------------

// handleTagsIndex renders the add-in tag catalog: a create form, the live tags
// (rename/archive), and a muted list of archived tags. PRG + methodOverride, so
// it works without JavaScript (consistent with the dogs page).
func (s *Server) handleTagsIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tags, err := s.store.ListTags(ctx, true)
	if err != nil {
		s.serverError(w, "tags", err)
		return
	}
	data := tagsData{baseData: s.base("tags"), Flash: readFlash(r)}
	for _, t := range tags {
		v := tagView{ID: t.ID, Name: t.Name, IsUnspecified: t.IsUnspecified}
		if t.ArchivedAt != nil {
			data.Archived = append(data.Archived, v)
		} else {
			data.Live = append(data.Live, v)
		}
	}
	if err := s.tmpl.render(w, http.StatusOK, "tags", data); err != nil {
		s.serverError(w, "tags", err)
	}
}

func (s *Server) handleTagCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.flashRedirect(w, r, "/tags", "err", "give the add-in a name")
		return
	}
	if len(name) > maxTagNameLen {
		s.flashRedirect(w, r, "/tags", "err", "that name is too long")
		return
	}
	_, err := s.store.CreateTag(r.Context(), name)
	if errors.Is(err, store.ErrTagNameTaken) {
		s.flashRedirect(w, r, "/tags", "err", "there's already an add-in with that name")
		return
	}
	if err != nil {
		s.log.Error("create tag", "err", err)
		s.flashRedirect(w, r, "/tags", "err", "couldn't add that add-in")
		return
	}
	s.flashRedirect(w, r, "/tags", "ok", "added "+name)
}

func (s *Server) handleTagUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > maxTagNameLen {
		s.flashRedirect(w, r, "/tags", "err", "that name won't work")
		return
	}
	err = s.store.RenameTag(r.Context(), id, name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.handleNotFound(w, r)
	case errors.Is(err, store.ErrTagNameTaken):
		s.flashRedirect(w, r, "/tags", "err", "there's already an add-in with that name")
	case err != nil:
		s.log.Error("rename tag", "id", id, "err", err)
		s.flashRedirect(w, r, "/tags", "err", "couldn't rename that add-in")
	default:
		s.flashRedirect(w, r, "/tags", "ok", "renamed to "+name)
	}
}

func (s *Server) handleTagDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	err = s.store.ArchiveTag(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.handleNotFound(w, r)
	case err != nil:
		s.log.Error("archive tag", "id", id, "err", err)
		s.flashRedirect(w, r, "/tags", "err", "couldn't archive that add-in")
	default:
		s.flashRedirect(w, r, "/tags", "ok", "archived (kept on past feedings)")
	}
}

// ----------------------------- feeding chips (HTMX) --------------------------

// handleFeedingTagAttach attaches an add-in to a feeding. The posted name is
// matched case-insensitively against the catalog and created on the fly if
// absent. Attaching a named add-in clears the reserved "Unspecified" sentinel
// if present — naming a device "Other" selection resolves it in one click.
func (s *Server) handleFeedingTagAttach(w http.ResponseWriter, r *http.Request) {
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
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.renderFeedingTags(w, ctx, fd)
		return
	}
	if len(name) > maxTagNameLen {
		s.htmxBanner(w, "#tag-msg-"+strconv.FormatInt(id, 10), "innerHTML", "that name is too long")
		return
	}
	tag, err := s.store.GetOrCreateTag(ctx, name)
	if err != nil {
		s.log.Error("get-or-create tag", "err", err)
		s.htmxBanner(w, "#tag-msg-"+strconv.FormatInt(id, 10), "innerHTML", "couldn't add that add-in")
		return
	}
	if err := s.store.AttachTag(ctx, id, tag.ID); err != nil {
		s.log.Error("attach tag", "feeding", id, "tag", tag.ID, "err", err)
		s.htmxBanner(w, "#tag-msg-"+strconv.FormatInt(id, 10), "innerHTML", "couldn't add that add-in")
		return
	}
	// Naming a real add-in resolves a pending "Unspecified" sentinel.
	if !tag.IsUnspecified {
		for _, existing := range fd.Tags {
			if existing.IsUnspecified {
				_ = s.store.DetachTag(ctx, id, store.UnspecifiedTagID)
				break
			}
		}
	}
	fd, _ = s.store.GetFeeding(ctx, id)
	s.renderFeedingTags(w, ctx, fd)
}

// handleFeedingTagDetach removes one add-in from a feeding.
func (s *Server) handleFeedingTagDetach(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	tagID, err := strconv.ParseInt(r.PathValue("tagID"), 10, 64)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	if err := s.store.DetachTag(ctx, id, tagID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("detach tag", "feeding", id, "tag", tagID, "err", err)
	}
	fd, err := s.store.GetFeeding(ctx, id)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	s.renderFeedingTags(w, ctx, fd)
}

func (s *Server) renderFeedingTags(w http.ResponseWriter, ctx context.Context, fd domain.Feeding) {
	if err := s.tmpl.fragment(w, http.StatusOK, "feedings", "tag_area", feedingTagsView(fd)); err != nil {
		s.serverError(w, "tag area", err)
	}
}

// feedingTagsView builds the chip/area view model from a loaded feeding.
func feedingTagsView(fd domain.Feeding) feedingTags {
	ft := feedingTags{FeedingID: fd.ID, DogID: fd.DogID}
	for _, t := range fd.Tags {
		ft.Tags = append(ft.Tags, tagChip{ID: t.ID, Name: t.Name, IsUnspecified: t.IsUnspecified})
		if t.IsUnspecified {
			ft.HasUnspecified = true
		}
	}
	return ft
}

// dogTagLists builds the per-dog ranked suggestion lists for the type-ahead
// datalists on the feedings page.
func (s *Server) dogTagLists(ctx context.Context, dogs []domain.Dog) []dogTagList {
	out := make([]dogTagList, 0, len(dogs))
	for _, d := range dogs {
		ranked, err := s.store.TagsForDog(ctx, d.ID)
		if err != nil {
			s.log.Warn("tags for dog", "dog", d.ID, "err", err)
			continue
		}
		names := make([]string, 0, len(ranked))
		for _, rt := range ranked {
			names = append(names, rt.Name)
		}
		out = append(out, dogTagList{DogID: d.ID, Names: names})
	}
	return out
}

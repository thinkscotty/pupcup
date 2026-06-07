package web

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	// Registered for image.DecodeConfig so we can read dimensions + format from
	// an upload header without decoding the full pixel buffer.
	_ "image/jpeg"
	_ "image/png"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

const (
	maxDogNameLen  = 40
	uploadOverhead = 1 << 16 // headroom over the photo limit for multipart framing + other fields
)

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validHexColor(s string) bool { return hexColorRe.MatchString(s) }

// ----------------------------- view models ----------------------------------

type dogsData struct {
	baseData
	Dogs       []dogManageView
	Palette    []swatch
	Flash      *flash
	PhotoMaxPx int // surfaced so the upload hint matches the validated limit
	PhotoMaxKB int
}

type dogManageView struct {
	ID            int64
	Name          string
	AccentColor   string
	HasPhoto      bool
	SortOrder     int
	ActiveEntries int
	CanDelete     bool
}

// swatch is a suggested accent color shown beside the color picker.
type swatch struct {
	Name string
	Hex  string
}

func accentPalette() []swatch {
	return []swatch{
		{"Green", "#A8D8B9"}, {"Yellow", "#F8D8A0"}, {"Red", "#F2A6A1"}, {"Blue", "#A8C8F8"},
		{"Lavender", "#C9B8E8"}, {"Mint", "#B9E8D8"}, {"Peach", "#F8C8A0"}, {"Stone", "#D8D2C8"},
	}
}

// ----------------------------- handlers --------------------------------------

func (s *Server) handleDogsIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dogs, err := s.store.ListDogs(ctx)
	if err != nil {
		s.serverError(w, "dogs", err)
		return
	}
	counts, err := s.store.ActiveEntryCounts(ctx)
	if err != nil {
		s.log.Warn("dogs entry counts", "err", err)
		counts = map[int64]int{}
	}

	data := dogsData{
		baseData:   s.base("dogs"),
		Palette:    accentPalette(),
		Flash:      readFlash(r),
		PhotoMaxPx: s.photoMaxPx,
		PhotoMaxKB: s.photoMaxKB,
	}
	for _, d := range dogs {
		n := counts[d.ID]
		data.Dogs = append(data.Dogs, dogManageView{
			ID:            d.ID,
			Name:          d.Name,
			AccentColor:   d.AccentColor,
			HasPhoto:      d.PhotoPath != "",
			SortOrder:     d.SortOrder,
			ActiveEntries: n,
			CanDelete:     n == 0,
		})
	}
	if err := s.tmpl.render(w, http.StatusOK, "dogs", data); err != nil {
		s.serverError(w, "dogs", err)
	}
}

func (s *Server) handleDogCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.parseUpload(w, r); err != nil {
		s.flashRedirect(w, r, "/dogs", "err", err.Error())
		return
	}
	name, color, sortOrder, _, err := parseDogForm(r)
	if err != nil {
		s.flashRedirect(w, r, "/dogs", "err", err.Error())
		return
	}
	data, ext, present, err := s.readPhoto(r)
	if err != nil {
		s.flashRedirect(w, r, "/dogs", "err", err.Error())
		return
	}

	dog, err := s.store.CreateDog(ctx, domain.Dog{Name: name, AccentColor: color, SortOrder: sortOrder})
	if err != nil {
		s.log.Error("create dog", "err", err)
		s.flashRedirect(w, r, "/dogs", "err", "couldn't add that dog")
		return
	}
	if present {
		rel, serr := s.savePhoto(dog.ID, data, ext)
		if serr != nil {
			s.log.Error("save photo", "dog", dog.ID, "err", serr)
			s.flashRedirect(w, r, "/dogs", "err", "added "+name+", but the photo couldn't be saved")
			return
		}
		dog.PhotoPath = rel
		if uerr := s.store.UpdateDog(ctx, dog); uerr != nil {
			s.log.Error("attach photo", "dog", dog.ID, "err", uerr)
		}
	}
	s.notifyDogsChanged()
	s.flashRedirect(w, r, "/dogs", "ok", "added "+name)
}

func (s *Server) handleDogUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	dog, err := s.store.GetDog(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.handleNotFound(w, r)
			return
		}
		s.serverError(w, "dog update", err)
		return
	}

	if err := s.parseUpload(w, r); err != nil {
		s.flashRedirect(w, r, "/dogs", "err", err.Error())
		return
	}
	name, color, sortOrder, removePhoto, err := parseDogForm(r)
	if err != nil {
		s.flashRedirect(w, r, "/dogs", "err", err.Error())
		return
	}
	data, ext, present, err := s.readPhoto(r)
	if err != nil {
		s.flashRedirect(w, r, "/dogs", "err", err.Error())
		return
	}

	dog.Name = name
	dog.AccentColor = color
	dog.SortOrder = sortOrder
	switch {
	case present:
		rel, serr := s.savePhoto(dog.ID, data, ext)
		if serr != nil {
			s.log.Error("save photo", "dog", dog.ID, "err", serr)
			s.flashRedirect(w, r, "/dogs", "err", "the photo couldn't be saved")
			return
		}
		dog.PhotoPath = rel
	case removePhoto && dog.PhotoPath != "":
		s.removePhotoFile(dog.PhotoPath)
		dog.PhotoPath = ""
	}

	if err := s.store.UpdateDog(ctx, dog); err != nil {
		s.log.Error("update dog", "dog", id, "err", err)
		s.flashRedirect(w, r, "/dogs", "err", "couldn't save changes")
		return
	}
	s.notifyDogsChanged()
	s.flashRedirect(w, r, "/dogs", "ok", "updated "+name)
}

func (s *Server) handleDogDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		s.handleNotFound(w, r)
		return
	}
	// Grab the photo path first so we can clean the file up after a successful
	// soft-delete (best-effort; a leftover file is harmless).
	var photoPath string
	if dog, gerr := s.store.GetDog(ctx, id); gerr == nil {
		photoPath = dog.PhotoPath
	}

	if err := s.store.SoftDeleteDog(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.flashRedirect(w, r, "/dogs", "err", "that dog is already gone")
			return
		}
		// The store blocks deletion while a dog has feeding/snack history.
		s.flashRedirect(w, r, "/dogs", "err", "can't remove a dog that still has feeding history")
		return
	}
	if photoPath != "" {
		s.removePhotoFile(photoPath)
	}
	s.notifyDogsChanged()
	s.flashRedirect(w, r, "/dogs", "ok", "removed dog")
}

// ----------------------------- form + photo helpers -------------------------

// parseUpload bounds the request body and parses the (multipart) form. A
// non-multipart POST is accepted too (no photo), so the dog forms degrade
// gracefully if a client omits the file part.
func (s *Server) parseUpload(w http.ResponseWriter, r *http.Request) error {
	limit := int64(s.photoMaxKB)*1024 + uploadOverhead
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(limit); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return fmt.Errorf("upload was too large or malformed (photos must be under %d KB)", s.photoMaxKB)
	}
	return nil
}

func parseDogForm(r *http.Request) (name, color string, sortOrder int, removePhoto bool, err error) {
	name = strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > maxDogNameLen {
		return "", "", 0, false, fmt.Errorf("a name (1–%d characters) is required", maxDogNameLen)
	}
	color = strings.TrimSpace(r.FormValue("accent_color"))
	if !validHexColor(color) {
		return "", "", 0, false, fmt.Errorf("pick a valid accent color")
	}
	if so := strings.TrimSpace(r.FormValue("sort_order")); so != "" {
		n, perr := strconv.Atoi(so)
		if perr != nil {
			return "", "", 0, false, fmt.Errorf("sort order must be a number")
		}
		sortOrder = n
	}
	removePhoto = r.FormValue("remove_photo") != ""
	return name, color, sortOrder, removePhoto, nil
}

// readPhoto pulls an optional "photo" file from the form and validates it
// against the configured format/size/dimension limits. present reports whether
// a (non-empty) file part was supplied; the returned bytes are nil when not.
func (s *Server) readPhoto(r *http.Request) (data []byte, ext string, present bool, err error) {
	f, _, ferr := r.FormFile("photo")
	if errors.Is(ferr, http.ErrMissingFile) {
		return nil, "", false, nil
	}
	if ferr != nil {
		return nil, "", false, fmt.Errorf("couldn't read the uploaded photo")
	}
	defer f.Close()

	maxBytes := int64(s.photoMaxKB) * 1024
	buf, rerr := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if rerr != nil {
		return nil, "", false, fmt.Errorf("couldn't read the uploaded photo")
	}
	if len(buf) == 0 {
		return nil, "", false, nil // empty file input — treat as "no photo"
	}
	if int64(len(buf)) > maxBytes {
		return nil, "", true, fmt.Errorf("photo is larger than %d KB", s.photoMaxKB)
	}
	if s.photoDir == "" {
		return nil, "", true, fmt.Errorf("photo uploads are not configured on this device")
	}

	cfg, format, derr := image.DecodeConfig(bytes.NewReader(buf))
	if derr != nil {
		return nil, "", true, fmt.Errorf("that file isn't a readable JPEG or PNG")
	}
	switch format {
	case "jpeg":
		ext = ".jpg"
	case "png":
		ext = ".png"
	default:
		return nil, "", true, fmt.Errorf("photos must be JPEG or PNG (got %s)", format)
	}
	if cfg.Width > s.photoMaxPx || cfg.Height > s.photoMaxPx {
		return nil, "", true, fmt.Errorf("photo is larger than %d×%d px (got %d×%d)",
			s.photoMaxPx, s.photoMaxPx, cfg.Width, cfg.Height)
	}
	return buf, ext, true, nil
}

// savePhoto writes the validated bytes as dog-<id>.<ext> under photoDir and
// returns the stored relative path. It removes a stale file in the other
// supported extension so a format switch doesn't orphan the old image.
func (s *Server) savePhoto(dogID int64, data []byte, ext string) (string, error) {
	if s.photoDir == "" {
		return "", fmt.Errorf("photo storage is not configured")
	}
	if err := os.MkdirAll(s.photoDir, 0o755); err != nil {
		return "", err
	}
	other := ".png"
	if ext == ".png" {
		other = ".jpg"
	}
	_ = os.Remove(filepath.Join(s.photoDir, fmt.Sprintf("dog-%d%s", dogID, other)))

	name := fmt.Sprintf("dog-%d%s", dogID, ext)
	if err := os.WriteFile(filepath.Join(s.photoDir, name), data, 0o644); err != nil {
		return "", err
	}
	return name, nil
}

// removePhotoFile deletes a stored photo (best-effort), guarding the join
// against path traversal in case photo_path is ever something unexpected.
func (s *Server) removePhotoFile(rel string) {
	if s.photoDir == "" || rel == "" {
		return
	}
	abs, err := safeJoin(s.photoDir, rel)
	if err != nil {
		return
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.log.Warn("remove photo file", "path", rel, "err", err)
	}
}

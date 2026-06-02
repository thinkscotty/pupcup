package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/scottyturner/pupcup/internal/store"
)

// handlePhoto serves a dog's photo from photoDir. The stored photo_path is
// joined under photoDir with a Clean + dir-prefix guard, and missing photos
// (no dog, no path, or no file) return a plain 404 — templates only emit the
// <img> when HasPhoto is set, so a 404 here is the uncommon case.
func (s *Server) handlePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dog, err := s.store.GetDog(r.Context(), id)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Warn("photo get dog", "dog", id, "err", err)
		}
		http.NotFound(w, r)
		return
	}
	if dog.PhotoPath == "" || s.photoDir == "" {
		http.NotFound(w, r)
		return
	}
	abs, err := safeJoin(s.photoDir, dog.PhotoPath)
	if err != nil {
		s.log.Warn("photo path rejected", "dog", id, "path", dog.PhotoPath, "err", err)
		http.NotFound(w, r)
		return
	}
	// Photos change rarely but can be re-uploaded; a short private cache keeps
	// the dashboard snappy without pinning a stale image for long.
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, abs)
}

// safeJoin resolves rel under dir, refusing any result that escapes dir. The
// `filepath.Clean("/"+rel)` step neutralizes leading "../" segments before the
// join; the prefix check is a second line of defense.
func safeJoin(dir, rel string) (string, error) {
	clean := filepath.Clean("/" + rel)
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	fileAbs, err := filepath.Abs(filepath.Join(dirAbs, clean))
	if err != nil {
		return "", err
	}
	if fileAbs != dirAbs && !strings.HasPrefix(fileAbs, dirAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes photo dir", rel)
	}
	return fileAbs, nil
}

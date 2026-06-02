package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/scottyturner/pupcup/internal/domain"
)

// UnspecifiedTagID is the reserved "Other / name later" sentinel (seeded by
// migration 0002 as id 1). Device "Other" selections attach it so the feeding
// is recorded immediately and surfaced on the web "needs a name" queue.
const UnspecifiedTagID int64 = 1

// ErrTagNameTaken is returned when a tag name collides with a live tag.
var ErrTagNameTaken = errors.New("tag name already in use")

// RankedTag is a live add-in tag with this dog's usage count, used to order
// the device and web pickers most-used-first.
type RankedTag struct {
	ID   int64
	Name string
	Uses int // how many of this dog's (non-deleted) feedings carry this tag
}

const feedTagSelect = `SELECT id, name, is_unspecified, archived_at, created_at FROM feed_tags`

func scanTag(s scanner) (domain.FeedTag, error) {
	var (
		t            domain.FeedTag
		unspecified  int
		arch, create sql.NullString
	)
	if err := s.Scan(&t.ID, &t.Name, &unspecified, &arch, &create); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.FeedTag{}, ErrNotFound
		}
		return domain.FeedTag{}, err
	}
	t.IsUnspecified = unspecified != 0
	var err error
	if t.ArchivedAt, err = scanNullableTime(arch); err != nil {
		return domain.FeedTag{}, err
	}
	if create.Valid {
		if ct, err := parseTime(create.String); err == nil {
			t.CreatedAt = ct
		}
	}
	return t, nil
}

// canonTagName collapses whitespace and Title-Cases a tag name for storage
// (the first letter of each word, including after a hyphen: "freeze-dried" →
// "Freeze-Dried"). Names are matched case-insensitively in the DB (NOCASE), so
// this only affects display.
func canonTagName(name string) string {
	lower := strings.ToLower(strings.Join(strings.Fields(name), " "))
	out := []rune(lower)
	atWordStart := true
	for i, r := range out {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if atWordStart {
				out[i] = unicode.ToUpper(r)
			}
			atWordStart = false
		default:
			atWordStart = true
		}
	}
	return string(out)
}

// ListTags returns the add-in catalog ordered by name. Archived tags are
// included only when includeArchived is set; the Unspecified sentinel is always
// included so the catalog page can show its history role.
func (s *Store) ListTags(ctx context.Context, includeArchived bool) ([]domain.FeedTag, error) {
	q := feedTagSelect
	if !includeArchived {
		q += " WHERE archived_at IS NULL"
	}
	q += " ORDER BY is_unspecified DESC, name COLLATE NOCASE ASC"
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FeedTag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTag returns one tag by id (including archived).
func (s *Store) GetTag(ctx context.Context, id int64) (domain.FeedTag, error) {
	row := s.db.QueryRowContext(ctx, feedTagSelect+` WHERE id = ?`, id)
	return scanTag(row)
}

// CreateTag inserts a new live tag with a canonicalized name. It returns
// ErrTagNameTaken if a live tag already has that name (case-insensitively).
func (s *Store) CreateTag(ctx context.Context, name string) (domain.FeedTag, error) {
	name = canonTagName(name)
	if name == "" {
		return domain.FeedTag{}, fmt.Errorf("tag name is required")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO feed_tags (name) VALUES (?)`, name)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.FeedTag{}, ErrTagNameTaken
		}
		return domain.FeedTag{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetTag(ctx, id)
}

// GetOrCreateTag returns the live tag matching name (case-insensitively),
// creating it if none exists. Used by the "create a tag on the fly" affordances.
func (s *Store) GetOrCreateTag(ctx context.Context, name string) (domain.FeedTag, error) {
	name = canonTagName(name)
	if name == "" {
		return domain.FeedTag{}, fmt.Errorf("tag name is required")
	}
	row := s.db.QueryRowContext(ctx,
		feedTagSelect+` WHERE archived_at IS NULL AND name = ? COLLATE NOCASE`, name)
	t, err := scanTag(row)
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return domain.FeedTag{}, err
	}
	return s.CreateTag(ctx, name)
}

// RenameTag changes a tag's display name. The Unspecified sentinel cannot be
// renamed. Returns ErrTagNameTaken on a live-name collision.
func (s *Store) RenameTag(ctx context.Context, id int64, name string) error {
	if id == UnspecifiedTagID {
		return fmt.Errorf("the reserved add-in tag cannot be renamed")
	}
	name = canonTagName(name)
	if name == "" {
		return fmt.Errorf("tag name is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE feed_tags SET name=? WHERE id=? AND is_unspecified=0`, name, id)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrTagNameTaken
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ArchiveTag soft-hides a tag from pickers without losing it from past
// feedings. The Unspecified sentinel cannot be archived.
func (s *Store) ArchiveTag(ctx context.Context, id int64) error {
	if id == UnspecifiedTagID {
		return fmt.Errorf("the reserved add-in tag cannot be archived")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE feed_tags SET archived_at=? WHERE id=? AND archived_at IS NULL AND is_unspecified=0`,
		formatTime(time.Now()), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TagsForDog returns live (non-archived) tags ranked by this dog's usage:
// per-dog use count DESC, then name ASC. The Unspecified sentinel is excluded;
// callers that need an "Other" affordance append it themselves.
func (s *Store) TagsForDog(ctx context.Context, dogID int64) ([]RankedTag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, COUNT(f.id) AS dog_uses
		FROM feed_tags t
		LEFT JOIN feeding_tags ft ON ft.tag_id = t.id
		LEFT JOIN feedings f ON f.id = ft.feeding_id
		     AND f.dog_id = ? AND f.deleted_at IS NULL
		WHERE t.archived_at IS NULL AND t.is_unspecified = 0
		GROUP BY t.id
		ORDER BY dog_uses DESC, t.name COLLATE NOCASE ASC`, dogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RankedTag
	for rows.Next() {
		var rt RankedTag
		if err := rows.Scan(&rt.ID, &rt.Name, &rt.Uses); err != nil {
			return nil, err
		}
		out = append(out, rt)
	}
	return out, rows.Err()
}

// AttachTag adds a tag to a feeding (idempotent). Returns ErrNotFound if either
// the feeding or tag does not exist.
func (s *Store) AttachTag(ctx context.Context, feedingID, tagID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO feeding_tags (feeding_id, tag_id) VALUES (?, ?)`, feedingID, tagID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// DetachTag removes a tag from a feeding.
func (s *Store) DetachTag(ctx context.Context, feedingID, tagID int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM feeding_tags WHERE feeding_id=? AND tag_id=?`, feedingID, tagID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetFeedingTags replaces the full set of tags on a feeding with tagIDs (in one
// transaction). Used by the web edit dialog's tag multiselect.
func (s *Store) SetFeedingTags(ctx context.Context, feedingID int64, tagIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM feeding_tags WHERE feeding_id=?`, feedingID); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, tid := range tagIDs {
		if seen[tid] {
			continue
		}
		seen[tid] = true
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO feeding_tags (feeding_id, tag_id) VALUES (?, ?)`, feedingID, tid); err != nil {
			if isForeignKeyViolation(err) {
				return ErrNotFound
			}
			return err
		}
	}
	return tx.Commit()
}

// TagsForFeeding returns the tags attached to one feeding, ordered by name.
func (s *Store) TagsForFeeding(ctx context.Context, feedingID int64) ([]domain.FeedTag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.is_unspecified, t.archived_at, t.created_at
		FROM feed_tags t
		JOIN feeding_tags ft ON ft.tag_id = t.id
		WHERE ft.feeding_id = ?
		ORDER BY t.is_unspecified DESC, t.name COLLATE NOCASE ASC`, feedingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FeedTag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// attachTagsToFeedings populates the Tags field on each feeding with one query
// (avoids N+1). No-op for an empty slice.
func (s *Store) attachTagsToFeedings(ctx context.Context, feeds []domain.Feeding) error {
	if len(feeds) == 0 {
		return nil
	}
	idx := make(map[int64]int, len(feeds))
	placeholders := make([]string, len(feeds))
	args := make([]any, len(feeds))
	for i := range feeds {
		idx[feeds[i].ID] = i
		placeholders[i] = "?"
		args[i] = feeds[i].ID
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ft.feeding_id, t.id, t.name, t.is_unspecified, t.archived_at, t.created_at
		FROM feeding_tags ft
		JOIN feed_tags t ON t.id = ft.tag_id
		WHERE ft.feeding_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY t.is_unspecified DESC, t.name COLLATE NOCASE ASC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			feedingID    int64
			t            domain.FeedTag
			unspecified  int
			arch, create sql.NullString
		)
		if err := rows.Scan(&feedingID, &t.ID, &t.Name, &unspecified, &arch, &create); err != nil {
			return err
		}
		t.IsUnspecified = unspecified != 0
		if t.ArchivedAt, err = scanNullableTime(arch); err != nil {
			return err
		}
		if create.Valid {
			if ct, err := parseTime(create.String); err == nil {
				t.CreatedAt = ct
			}
		}
		if i, ok := idx[feedingID]; ok {
			feeds[i].Tags = append(feeds[i].Tags, t)
		}
	}
	return rows.Err()
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// isForeignKeyViolation reports whether err is a SQLite FK constraint failure.
func isForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// Package store is the SQLite persistence layer for PupCup. It owns the
// database connection, runs migrations on Open, and exposes CRUD methods
// returning domain types. Times are stored as UTC RFC3339 strings.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/migrations"

	_ "modernc.org/sqlite"
)

const dbTimeFmt = "2006-01-02T15:04:05.000Z07:00"

// ErrNotFound is returned when a row is missing.
var ErrNotFound = errors.New("not found")

// Store wraps a *sql.DB with the PupCup schema.
type Store struct {
	db   *sql.DB
	path string // empty if in-memory
}

// Open opens or creates a SQLite database at path, applies migrations from
// the embedded migrations.FS, and returns a *Store. Pass ":memory:" for an
// ephemeral test DB.
func Open(path string) (*Store, error) {
	dsn := buildDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	// SQLite is single-writer; serialize writes to avoid SQLITE_BUSY under
	// the in-memory event-bus + web-handler concurrency we run.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA synchronous = NORMAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pragma: %w", err)
	}

	s := &Store{db: db}
	if path != ":memory:" && path != "" {
		s.path = path
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func buildDSN(path string) string {
	if path == "" || path == ":memory:" {
		// Shared cache so multiple goroutines using one *sql.DB see the same
		// in-memory DB. _pragma honored by modernc driver.
		return "file::memory:?cache=shared&_pragma=foreign_keys(1)"
	}
	v := url.Values{}
	v.Set("_pragma", "foreign_keys(1)")
	return "file:" + path + "?" + v.Encode()
}

// Close releases the underlying DB.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying *sql.DB for advanced callers (chart helpers,
// tests). Most callers should use the typed methods.
func (s *Store) DB() *sql.DB { return s.db }

// SizeBytes returns the on-disk size of the database file, or 0 for an
// in-memory store. Used by /healthz. A missing file (not yet created) is 0.
func (s *Store) SizeBytes() (int64, error) {
	if s.path == "" {
		return 0, nil
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return fi.Size(), nil
}

// LastButtonEventTime returns the timestamp of the most recent non-deleted
// device-sourced (button) feeding or snack, or nil if none exists. Used by
// /healthz as a device-liveness signal without subscribing to the event bus.
func (s *Store) LastButtonEventTime(ctx context.Context) (*time.Time, error) {
	var ts sql.NullString
	row := s.db.QueryRowContext(ctx, `
		SELECT MAX(ts_utc) FROM (
			SELECT ts_utc FROM feedings WHERE source='button' AND deleted_at IS NULL
			UNION ALL
			SELECT ts_utc FROM snacks   WHERE source='button' AND deleted_at IS NULL
		)`)
	if err := row.Scan(&ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return scanNullableTime(ts)
}

// ----------------------------- migrations -----------------------------------

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query schema_migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	hasUnapplied := false
	for _, f := range files {
		if !applied[f] {
			hasUnapplied = true
			break
		}
	}
	if hasUnapplied && s.path != "" {
		if err := s.backup(); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
	}

	for _, f := range files {
		if applied[f] {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", f, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, f); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", f, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backup() error {
	src, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // first-run: nothing to back up
		}
		return err
	}
	defer src.Close()
	stamp := time.Now().UTC().Format("20060102-150405")
	dst := filepath.Join(filepath.Dir(s.path), filepath.Base(s.path)+".bak."+stamp)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(src); err != nil {
		return err
	}
	return out.Sync()
}

// ----------------------------- helpers --------------------------------------

func formatTime(t time.Time) string {
	return t.UTC().Format(dbTimeFmt)
}

func parseTime(s string) (time.Time, error) {
	// Accept the canonical format and the SQLite default (no fractional, no TZ).
	for _, layout := range []string{dbTimeFmt, "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parseTime: unrecognized format %q", s)
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func scanNullableTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// ----------------------------- dogs -----------------------------------------

// CreateDog inserts a dog and returns the populated row (with ID & CreatedAt).
func (s *Store) CreateDog(ctx context.Context, d domain.Dog) (domain.Dog, error) {
	if err := d.Validate(); err != nil {
		return domain.Dog{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO dogs (name, accent_color, photo_path, sort_order) VALUES (?, ?, ?, ?)`,
		d.Name, d.AccentColor, nullableString(d.PhotoPath), d.SortOrder)
	if err != nil {
		return domain.Dog{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Dog{}, err
	}
	return s.GetDog(ctx, id)
}

// GetDog returns a dog by id.
func (s *Store) GetDog(ctx context.Context, id int64) (domain.Dog, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, accent_color, COALESCE(photo_path,''), sort_order, created_at
		   FROM dogs WHERE id = ? AND deleted_at IS NULL`, id)
	return scanDog(row)
}

// ListDogs returns all non-deleted dogs ordered by sort_order then id.
func (s *Store) ListDogs(ctx context.Context) ([]domain.Dog, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, accent_color, COALESCE(photo_path,''), sort_order, created_at
		   FROM dogs WHERE deleted_at IS NULL ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Dog
	for rows.Next() {
		d, err := scanDog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateDog updates name, accent color, photo path, and sort order.
func (s *Store) UpdateDog(ctx context.Context, d domain.Dog) error {
	if err := d.Validate(); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE dogs SET name=?, accent_color=?, photo_path=?, sort_order=? WHERE id=? AND deleted_at IS NULL`,
		d.Name, d.AccentColor, nullableString(d.PhotoPath), d.SortOrder, d.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDeleteDog marks a dog deleted iff it has no non-deleted feedings/snacks.
func (s *Store) SoftDeleteDog(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM feedings WHERE dog_id=? AND deleted_at IS NULL)
		      + (SELECT COUNT(*) FROM snacks   WHERE dog_id=? AND deleted_at IS NULL)`,
		id, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("dog %d has %d active entries; cannot delete", id, n)
	}
	res, err := tx.ExecContext(ctx, `UPDATE dogs SET deleted_at=? WHERE id=? AND deleted_at IS NULL`,
		formatTime(time.Now()), id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// ActiveEntryCounts returns, per dog id, the number of non-deleted feedings
// plus snacks. The dogs-management page uses it to show why a dog can't yet be
// deleted (SoftDeleteDog enforces the same rule transactionally). One grouped
// query rather than a count per dog. Dogs with zero entries are simply absent
// from the map.
func (s *Store) ActiveEntryCounts(ctx context.Context) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT dog_id, COUNT(*) FROM (
			SELECT dog_id FROM feedings WHERE deleted_at IS NULL
			UNION ALL
			SELECT dog_id FROM snacks   WHERE deleted_at IS NULL
		) GROUP BY dog_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]int)
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(...any) error
}

func scanDog(s scanner) (domain.Dog, error) {
	var (
		d       domain.Dog
		created string
	)
	if err := s.Scan(&d.ID, &d.Name, &d.AccentColor, &d.PhotoPath, &d.SortOrder, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Dog{}, ErrNotFound
		}
		return domain.Dog{}, err
	}
	t, err := parseTime(created)
	if err != nil {
		return domain.Dog{}, err
	}
	d.CreatedAt = t
	return d, nil
}

// ----------------------------- feedings -------------------------------------

// CreateFeeding inserts a feeding and returns the populated row.
func (s *Store) CreateFeeding(ctx context.Context, f domain.Feeding) (domain.Feeding, error) {
	if err := f.Validate(); err != nil {
		return domain.Feeding{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO feedings (dog_id, ts_utc, kind, score, specifics, source) VALUES (?, ?, ?, ?, ?, ?)`,
		f.DogID, formatTime(f.TS), string(f.Kind), string(f.Score), nullableString(f.Specifics), string(f.Source))
	if err != nil {
		return domain.Feeding{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetFeeding(ctx, id)
}

// GetFeeding returns a feeding by id (including soft-deleted), with its add-in
// tags populated.
func (s *Store) GetFeeding(ctx context.Context, id int64) (domain.Feeding, error) {
	row := s.db.QueryRowContext(ctx, feedingSelect+` WHERE id = ?`, id)
	f, err := scanFeeding(row)
	if err != nil {
		return domain.Feeding{}, err
	}
	if f.Tags, err = s.TagsForFeeding(ctx, f.ID); err != nil {
		return domain.Feeding{}, err
	}
	return f, nil
}

const feedingSelect = `SELECT id, dog_id, ts_utc, kind, score, COALESCE(specifics,''), source, deleted_at, edited_at, created_at FROM feedings`

// FeedingFilter narrows the result set for ListFeedings.
type FeedingFilter struct {
	DogID          int64 // 0 = any
	Since, Until   time.Time
	IncludeDeleted bool
	Limit          int // 0 = no limit
}

// ListFeedings returns feedings matching the filter, newest first.
func (s *Store) ListFeedings(ctx context.Context, f FeedingFilter) ([]domain.Feeding, error) {
	var (
		conds []string
		args  []any
	)
	if !f.IncludeDeleted {
		conds = append(conds, "deleted_at IS NULL")
	}
	if f.DogID != 0 {
		conds = append(conds, "dog_id = ?")
		args = append(args, f.DogID)
	}
	if !f.Since.IsZero() {
		conds = append(conds, "ts_utc >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		conds = append(conds, "ts_utc < ?")
		args = append(args, formatTime(f.Until))
	}
	q := feedingSelect
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY ts_utc DESC, id DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Feeding
	for rows.Next() {
		fd, err := scanFeeding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fd)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.attachTagsToFeedings(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateFeeding edits ts, kind, score, specifics. Sets edited_at.
func (s *Store) UpdateFeeding(ctx context.Context, f domain.Feeding) error {
	if err := f.Validate(); err != nil {
		return err
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE feedings SET ts_utc=?, kind=?, score=?, specifics=?, edited_at=? WHERE id=? AND deleted_at IS NULL`,
		formatTime(f.TS), string(f.Kind), string(f.Score), nullableString(f.Specifics), formatTime(now), f.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDeleteFeeding marks a feeding deleted.
func (s *Store) SoftDeleteFeeding(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE feedings SET deleted_at=? WHERE id=? AND deleted_at IS NULL`,
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

func scanFeeding(s scanner) (domain.Feeding, error) {
	var (
		f                   domain.Feeding
		ts, created         string
		del, edit           sql.NullString
		kind, score, source string
	)
	if err := s.Scan(&f.ID, &f.DogID, &ts, &kind, &score, &f.Specifics, &source, &del, &edit, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Feeding{}, ErrNotFound
		}
		return domain.Feeding{}, err
	}
	t, err := parseTime(ts)
	if err != nil {
		return domain.Feeding{}, err
	}
	f.TS = t
	f.Kind = domain.FeedKind(kind)
	f.Score = domain.Score(score)
	f.Source = domain.Source(source)
	if t, err := parseTime(created); err == nil {
		f.CreatedAt = t
	}
	if f.DeletedAt, err = scanNullableTime(del); err != nil {
		return domain.Feeding{}, err
	}
	if f.EditedAt, err = scanNullableTime(edit); err != nil {
		return domain.Feeding{}, err
	}
	return f, nil
}

// ----------------------------- snacks ---------------------------------------

const snackSelect = `SELECT id, dog_id, ts_utc, COALESCE(specifics,''), source, deleted_at, edited_at, created_at FROM snacks`

// CreateSnack inserts a snack.
func (s *Store) CreateSnack(ctx context.Context, sn domain.Snack) (domain.Snack, error) {
	if err := sn.Validate(); err != nil {
		return domain.Snack{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO snacks (dog_id, ts_utc, specifics, source) VALUES (?, ?, ?, ?)`,
		sn.DogID, formatTime(sn.TS), nullableString(sn.Specifics), string(sn.Source))
	if err != nil {
		return domain.Snack{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetSnack(ctx, id)
}

// GetSnack returns a snack by id.
func (s *Store) GetSnack(ctx context.Context, id int64) (domain.Snack, error) {
	row := s.db.QueryRowContext(ctx, snackSelect+` WHERE id = ?`, id)
	return scanSnack(row)
}

// SnackFilter mirrors FeedingFilter.
type SnackFilter struct {
	DogID          int64
	Since, Until   time.Time
	IncludeDeleted bool
	Limit          int
}

// ListSnacks returns snacks matching the filter, newest first.
func (s *Store) ListSnacks(ctx context.Context, f SnackFilter) ([]domain.Snack, error) {
	var (
		conds []string
		args  []any
	)
	if !f.IncludeDeleted {
		conds = append(conds, "deleted_at IS NULL")
	}
	if f.DogID != 0 {
		conds = append(conds, "dog_id = ?")
		args = append(args, f.DogID)
	}
	if !f.Since.IsZero() {
		conds = append(conds, "ts_utc >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		conds = append(conds, "ts_utc < ?")
		args = append(args, formatTime(f.Until))
	}
	q := snackSelect
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY ts_utc DESC, id DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Snack
	for rows.Next() {
		sn, err := scanSnack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// UpdateSnack edits ts and specifics.
func (s *Store) UpdateSnack(ctx context.Context, sn domain.Snack) error {
	if err := sn.Validate(); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE snacks SET ts_utc=?, specifics=?, edited_at=? WHERE id=? AND deleted_at IS NULL`,
		formatTime(sn.TS), nullableString(sn.Specifics), formatTime(time.Now()), sn.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDeleteSnack marks a snack deleted.
func (s *Store) SoftDeleteSnack(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE snacks SET deleted_at=? WHERE id=? AND deleted_at IS NULL`,
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

func scanSnack(s scanner) (domain.Snack, error) {
	var (
		sn          domain.Snack
		ts, created string
		del, edit   sql.NullString
		source      string
	)
	if err := s.Scan(&sn.ID, &sn.DogID, &ts, &sn.Specifics, &source, &del, &edit, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Snack{}, ErrNotFound
		}
		return domain.Snack{}, err
	}
	t, err := parseTime(ts)
	if err != nil {
		return domain.Snack{}, err
	}
	sn.TS = t
	sn.Source = domain.Source(source)
	if t, err := parseTime(created); err == nil {
		sn.CreatedAt = t
	}
	if sn.DeletedAt, err = scanNullableTime(del); err != nil {
		return domain.Snack{}, err
	}
	if sn.EditedAt, err = scanNullableTime(edit); err != nil {
		return domain.Snack{}, err
	}
	return sn, nil
}

// ----------------------------- illness --------------------------------------

const illnessSelect = `SELECT id, dog_id, start_date, end_date, COALESCE(notes,''), created_at FROM illness_events`

// CreateIllness inserts an illness event.
func (s *Store) CreateIllness(ctx context.Context, e domain.IllnessEvent) (domain.IllnessEvent, error) {
	if e.DogID == 0 || e.Start.IsZero() {
		return domain.IllnessEvent{}, errors.New("illness: dog_id and start are required")
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO illness_events (dog_id, start_date, end_date, notes) VALUES (?, ?, ?, ?)`,
		e.DogID, e.Start.UTC().Format("2006-01-02"), nullableTime(e.End), nullableString(e.Notes))
	if err != nil {
		return domain.IllnessEvent{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetIllness(ctx, id)
}

// GetIllness returns an illness event by id.
func (s *Store) GetIllness(ctx context.Context, id int64) (domain.IllnessEvent, error) {
	row := s.db.QueryRowContext(ctx, illnessSelect+` WHERE id = ?`, id)
	return scanIllness(row)
}

// ListIllness returns illness events ordered by start desc.
func (s *Store) ListIllness(ctx context.Context, dogID int64) ([]domain.IllnessEvent, error) {
	q := illnessSelect
	var args []any
	if dogID != 0 {
		q += ` WHERE dog_id = ?`
		args = append(args, dogID)
	}
	q += ` ORDER BY start_date DESC, id DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.IllnessEvent
	for rows.Next() {
		e, err := scanIllness(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateIllness edits start, end, notes.
func (s *Store) UpdateIllness(ctx context.Context, e domain.IllnessEvent) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE illness_events SET start_date=?, end_date=?, notes=? WHERE id=?`,
		e.Start.UTC().Format("2006-01-02"), nullableTime(e.End), nullableString(e.Notes), e.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteIllness hard-deletes (these are typically annotations, no soft delete).
func (s *Store) DeleteIllness(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM illness_events WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanIllness(s scanner) (domain.IllnessEvent, error) {
	var (
		e       domain.IllnessEvent
		start   string
		end     sql.NullString
		created string
	)
	if err := s.Scan(&e.ID, &e.DogID, &start, &end, &e.Notes, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.IllnessEvent{}, ErrNotFound
		}
		return domain.IllnessEvent{}, err
	}
	t, err := parseTime(start)
	if err != nil {
		return domain.IllnessEvent{}, err
	}
	e.Start = t
	if e.End, err = scanNullableTime(end); err != nil {
		return domain.IllnessEvent{}, err
	}
	if t, err := parseTime(created); err == nil {
		e.CreatedAt = t
	}
	return e, nil
}

// ----------------------------- stress --------------------------------------

const stressSelect = `SELECT id, dog_id, start_date, end_date, COALESCE(kind,''), COALESCE(notes,''), created_at FROM stress_events`

// CreateStress inserts a stress event. DogID nil = whole household.
func (s *Store) CreateStress(ctx context.Context, e domain.StressEvent) (domain.StressEvent, error) {
	if e.Start.IsZero() {
		return domain.StressEvent{}, errors.New("stress: start is required")
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO stress_events (dog_id, start_date, end_date, kind, notes) VALUES (?, ?, ?, ?, ?)`,
		nullableInt64(e.DogID), e.Start.UTC().Format("2006-01-02"), nullableTime(e.End),
		nullableString(e.Kind), nullableString(e.Notes))
	if err != nil {
		return domain.StressEvent{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetStress(ctx, id)
}

// GetStress returns a stress event by id.
func (s *Store) GetStress(ctx context.Context, id int64) (domain.StressEvent, error) {
	row := s.db.QueryRowContext(ctx, stressSelect+` WHERE id = ?`, id)
	return scanStress(row)
}

// ListStress returns stress events ordered by start desc. dogID 0 = all
// (including household-wide).
func (s *Store) ListStress(ctx context.Context, dogID int64) ([]domain.StressEvent, error) {
	q := stressSelect
	var args []any
	if dogID != 0 {
		q += ` WHERE dog_id = ? OR dog_id IS NULL`
		args = append(args, dogID)
	}
	q += ` ORDER BY start_date DESC, id DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.StressEvent
	for rows.Next() {
		e, err := scanStress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateStress edits start, end, kind, notes.
func (s *Store) UpdateStress(ctx context.Context, e domain.StressEvent) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE stress_events SET dog_id=?, start_date=?, end_date=?, kind=?, notes=? WHERE id=?`,
		nullableInt64(e.DogID), e.Start.UTC().Format("2006-01-02"), nullableTime(e.End),
		nullableString(e.Kind), nullableString(e.Notes), e.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteStress hard-deletes a stress event.
func (s *Store) DeleteStress(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM stress_events WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanStress(s scanner) (domain.StressEvent, error) {
	var (
		e       domain.StressEvent
		dogID   sql.NullInt64
		start   string
		end     sql.NullString
		created string
	)
	if err := s.Scan(&e.ID, &dogID, &start, &end, &e.Kind, &e.Notes, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.StressEvent{}, ErrNotFound
		}
		return domain.StressEvent{}, err
	}
	if dogID.Valid {
		v := dogID.Int64
		e.DogID = &v
	}
	t, err := parseTime(start)
	if err != nil {
		return domain.StressEvent{}, err
	}
	e.Start = t
	if e.End, err = scanNullableTime(end); err != nil {
		return domain.StressEvent{}, err
	}
	if t, err := parseTime(created); err == nil {
		e.CreatedAt = t
	}
	return e, nil
}

// ----------------------------- device_state ---------------------------------

// GetDeviceLock reads the persisted lock state.
func (s *Store) GetDeviceLock(ctx context.Context) (domain.DeviceLock, error) {
	var (
		until  sql.NullString
		reason sql.NullString
	)
	row := s.db.QueryRowContext(ctx, `SELECT locked_until_utc, last_lock_reason FROM device_state WHERE id = 1`)
	if err := row.Scan(&until, &reason); err != nil {
		return domain.DeviceLock{}, err
	}
	var lock domain.DeviceLock
	t, err := scanNullableTime(until)
	if err != nil {
		return domain.DeviceLock{}, err
	}
	lock.Until = t
	if reason.Valid {
		lock.Reason = reason.String
	}
	return lock, nil
}

// SetDeviceLock persists the lock state. Pass nil Until to clear.
func (s *Store) SetDeviceLock(ctx context.Context, lock domain.DeviceLock) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE device_state SET locked_until_utc=?, last_lock_reason=?, updated_at=? WHERE id=1`,
		nullableTime(lock.Until), nullableString(lock.Reason), formatTime(time.Now()))
	return err
}

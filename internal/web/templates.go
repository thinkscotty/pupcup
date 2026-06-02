package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// pages maps a logical page name to the template file it is built from. Each
// page is parsed together with base.html into its own template set so the
// {{define "content"}} blocks don't collide across pages.
var pages = map[string]string{
	"dashboard": "templates/dashboard.html",
	"dogs":      "templates/dogs.html",
	"feedings":  "templates/feedings.html",
	"illness":   "templates/illness.html",
	"stress":    "templates/stress.html",
	"404":       "templates/404.html",
}

// templates holds one fully-parsed *template.Template per page, keyed by the
// page name. Parsed once at construction; rendering is concurrency-safe.
type templates struct {
	set map[string]*template.Template
}

func newTemplates(loc *time.Location) (*templates, error) {
	funcs := template.FuncMap{
		"fmtTime": func(t time.Time) string {
			if loc != nil {
				t = t.In(loc)
			}
			return t.Format("Mon 3:04 PM")
		},
		"fmtDate": func(t time.Time) string {
			if loc != nil {
				t = t.In(loc)
			}
			return t.Format("Jan 2, 2006")
		},
		// fmtClock is the bare wall-clock time used in dashboard status lines
		// ("fed at 8:15 AM").
		"fmtClock": func(t time.Time) string {
			if loc != nil {
				t = t.In(loc)
			}
			return t.Format("3:04 PM")
		},
		// fmtInputDateTime renders a UTC instant as the local wall-clock value an
		// <input type="datetime-local"> expects, for pre-filling the edit picker.
		"fmtInputDateTime": func(t time.Time) string {
			if loc != nil {
				t = t.In(loc)
			}
			return t.Format("2006-01-02T15:04")
		},
		// fmtEventDate renders a stored calendar date (illness/stress) for
		// display. These are date-only values kept in UTC, so format in UTC to
		// avoid a timezone shifting the day by one.
		"fmtEventDate": func(t time.Time) string {
			return t.UTC().Format("Mon, Jan 2 2006")
		},
		// fmtInputDate renders a stored calendar date as the value an
		// <input type="date"> expects, for pre-filling edit pickers.
		"fmtInputDate": func(t time.Time) string {
			return t.UTC().Format("2006-01-02")
		},
		"scoreLabel": scoreLabel,
		"scoreClass": scoreClass,
	}
	set := make(map[string]*template.Template, len(pages))
	for name, file := range pages {
		t, err := template.New("base.html").Funcs(funcs).
			ParseFS(templateFS, "templates/base.html", file)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		set[name] = t
	}
	return &templates{set: set}, nil
}

// scoreLabel turns an eating-quality score into the friendly phrase shown to
// the household.
func scoreLabel(s domain.Score) string {
	switch s {
	case domain.ScoreFull:
		return "Cleaned the bowl"
	case domain.ScorePartial:
		return "Ate some"
	case domain.ScoreNone:
		return "Didn't eat"
	default:
		return string(s)
	}
}

// scoreClass maps a score to a CSS modifier class for color-coding.
func scoreClass(s domain.Score) string {
	switch s {
	case domain.ScoreFull:
		return "score-full"
	case domain.ScorePartial:
		return "score-partial"
	case domain.ScoreNone:
		return "score-none"
	default:
		return ""
	}
}

// render executes the named page's "base" template into a buffer first, so a
// template error becomes a 500 instead of a half-written 200 body.
func (t *templates) render(w http.ResponseWriter, status int, page string, data any) error {
	tmpl, ok := t.set[page]
	if !ok {
		return fmt.Errorf("unknown page %q", page)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// fragment executes a single named {{define}} block from a page's template set
// (rather than the whole "base" layout), for HTMX partial responses. Buffered
// like render so a template error becomes a 500, not a torn fragment.
func (t *templates) fragment(w http.ResponseWriter, status int, page, name string, data any) error {
	tmpl, ok := t.set[page]
	if !ok {
		return fmt.Errorf("unknown page %q", page)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

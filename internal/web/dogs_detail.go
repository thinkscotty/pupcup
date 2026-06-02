package web

import (
	"errors"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
	"github.com/scottyturner/pupcup/internal/web/chart"
)

// detailWindows are the selectable look-back ranges (in days) on the per-dog
// detail page. 30 is the default.
var detailWindows = []int{7, 30, 90}

const defaultDetailWindow = 30

// ----------------------------- view models ----------------------------------

type dogDetailData struct {
	baseData
	Dog        dogHeader
	Window     int
	Windows    []int
	Chart      template.HTML
	Stats      mealStats
	SnackCount int
	Rows       []detailRow
}

type dogHeader struct {
	ID          int64
	Name        string
	AccentColor string
	HasPhoto    bool
}

// mealStats summarizes the meals in the window: counts per score plus their
// share of the total (rounded to whole percent).
type mealStats struct {
	Total      int
	Full       int
	Partial    int
	None       int
	FullPct    int
	PartialPct int
	NonePct    int
}

// detailRow is one entry in the read-only per-dog history table (meals and
// snacks merged, newest first). Edits live on the /feedings page.
type detailRow struct {
	When    time.Time
	IsSnack bool
	Score   domain.Score
	Kind    domain.FeedKind
	Detail  string
}

// ----------------------------- handler --------------------------------------

// handleDogDetail renders one dog's detail page: an eating-quality stacked-bar
// chart over a selectable window (7/30/90 days), summary stats, and a read-only
// meal/snack history table. The window is chosen via a plain ?window= GET param
// so the page works without JavaScript and is bookmarkable.
func (s *Server) handleDogDetail(w http.ResponseWriter, r *http.Request) {
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
		s.serverError(w, "dog detail", err)
		return
	}

	window := parseWindow(r.URL.Query().Get("window"))
	// Window covers `window` local days ending today. startLocal is local
	// midnight of the first day; the store filters on the equivalent UTC instant.
	startLocal := startOfLocalDay(s.clk.Now().In(s.loc)).AddDate(0, 0, -(window - 1))
	sinceUTC := startLocal.UTC()

	feeds, err := s.store.ListFeedings(ctx, store.FeedingFilter{DogID: id, Since: sinceUTC})
	if err != nil {
		s.serverError(w, "dog detail", err)
		return
	}
	snacks, err := s.store.ListSnacks(ctx, store.SnackFilter{DogID: id, Since: sinceUTC})
	if err != nil {
		s.serverError(w, "dog detail", err)
		return
	}

	days := buildChartDays(startLocal, window, s.loc, feeds)

	data := dogDetailData{
		baseData:   s.base("dogs"), // reached from the Dogs list; keep that tab active
		Dog:        dogHeader{ID: dog.ID, Name: dog.Name, AccentColor: dog.AccentColor, HasPhoto: dog.PhotoPath != ""},
		Window:     window,
		Windows:    detailWindows,
		Chart:      chart.StackedBar(days, 720, 220),
		Stats:      mealStatsFrom(feeds),
		SnackCount: len(snacks),
		Rows:       detailRows(feeds, snacks),
	}

	if err := s.tmpl.render(w, http.StatusOK, "dog_detail", data); err != nil {
		s.serverError(w, "dog detail", err)
	}
}

// parseWindow accepts only the offered windows, defaulting anything else to 30.
func parseWindow(v string) int {
	switch strings.TrimSpace(v) {
	case "7":
		return 7
	case "90":
		return 90
	default:
		return defaultDetailWindow
	}
}

// buildChartDays bins feedings into one bucket per day across the window,
// tallying meals by score. Days with no meals stay zeroed so the chart shows the
// full timeline (gaps included). Feedings are bucketed by their household-local
// day so a meal lands on the day the household saw it.
func buildChartDays(start time.Time, window int, loc *time.Location, feeds []domain.Feeding) []chart.Day {
	days := make([]chart.Day, window)
	idx := make(map[string]int, window)
	for i := 0; i < window; i++ {
		d := start.AddDate(0, 0, i)
		days[i] = chart.Day{Date: d}
		idx[d.Format("2006-01-02")] = i
	}
	for _, f := range feeds {
		i, ok := idx[f.TS.In(loc).Format("2006-01-02")]
		if !ok {
			continue
		}
		switch f.Score {
		case domain.ScoreFull:
			days[i].Full++
		case domain.ScorePartial:
			days[i].Partial++
		case domain.ScoreNone:
			days[i].None++
		}
	}
	return days
}

// mealStatsFrom tallies meals by score and computes each score's whole-percent
// share of the total.
func mealStatsFrom(feeds []domain.Feeding) mealStats {
	var st mealStats
	for _, f := range feeds {
		switch f.Score {
		case domain.ScoreFull:
			st.Full++
		case domain.ScorePartial:
			st.Partial++
		case domain.ScoreNone:
			st.None++
		}
	}
	st.Total = st.Full + st.Partial + st.None
	if st.Total > 0 {
		st.FullPct = pct(st.Full, st.Total)
		st.PartialPct = pct(st.Partial, st.Total)
		st.NonePct = pct(st.None, st.Total)
	}
	return st
}

func pct(n, total int) int {
	if total == 0 {
		return 0
	}
	return (n*100 + total/2) / total // rounded
}

// detailRows merges this dog's meals and snacks into one newest-first table.
// Both inputs arrive newest-first from the store; the stable sort keeps that
// order across the merge.
func detailRows(feeds []domain.Feeding, snacks []domain.Snack) []detailRow {
	rows := make([]detailRow, 0, len(feeds)+len(snacks))
	for _, f := range feeds {
		rows = append(rows, detailRow{When: f.TS, Score: f.Score, Kind: f.Kind, Detail: f.Specifics})
	}
	for _, sn := range snacks {
		rows = append(rows, detailRow{When: sn.TS, IsSnack: true, Detail: sn.Specifics})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].When.After(rows[j].When) })
	return rows
}

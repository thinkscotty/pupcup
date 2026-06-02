package chart

import (
	"strings"
	"testing"
	"time"
)

func days(n int) []Day {
	out := make([]Day, n)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = Day{Date: base.AddDate(0, 0, i)}
	}
	return out
}

func TestStackedBar_EmptySeriesShowsEmptyState(t *testing.T) {
	got := string(StackedBar(nil, 720, 220))
	if !strings.Contains(got, "No meals recorded") {
		t.Errorf("empty series should render an empty-state label; got %q", got)
	}
	if strings.Contains(got, "<rect class=\"bar") {
		t.Error("empty series should not draw bars")
	}
}

func TestStackedBar_AllZeroDaysShowsEmptyState(t *testing.T) {
	got := string(StackedBar(days(30), 720, 220))
	if !strings.Contains(got, "No meals recorded") {
		t.Error("a window with no meals should render the empty-state label")
	}
}

func TestStackedBar_RendersOneSegmentPerScore(t *testing.T) {
	d := days(3)
	d[1] = Day{Date: d[1].Date, Full: 2, Partial: 1, None: 1}
	got := string(StackedBar(d, 720, 220))

	for _, cls := range []string{"bar-full", "bar-partial", "bar-none"} {
		if !strings.Contains(got, cls) {
			t.Errorf("expected a %s segment in the chart", cls)
		}
	}
	// y-axis max label should equal the tallest day's total (4).
	if !strings.Contains(got, `text-anchor="end">4</text>`) {
		t.Errorf("expected a y-axis max label of 4; got %q", got)
	}
	// Per-bar tooltip should report that day's counts.
	if !strings.Contains(got, "2 full, 1 partial, 1 none") {
		t.Error("expected a per-bar tooltip with the day's counts")
	}
}

func TestStackedBar_OmitsAbsentSegments(t *testing.T) {
	d := days(1)
	d[0] = Day{Date: d[0].Date, Full: 3} // only full meals
	got := string(StackedBar(d, 720, 220))

	if !strings.Contains(got, "bar-full") {
		t.Error("expected a full segment")
	}
	if strings.Contains(got, "bar-partial") || strings.Contains(got, "bar-none") {
		t.Error("a full-only day should not draw partial/none segments")
	}
}

func TestStackedBar_IsWellFormedSVG(t *testing.T) {
	got := string(StackedBar(days(7), 720, 220))
	if !strings.HasPrefix(got, "<svg") || !strings.HasSuffix(got, "</svg>") {
		t.Errorf("output should be a single <svg> element; got %q", got)
	}
	if !strings.Contains(got, `viewBox="0 0 720 220"`) {
		t.Error("viewBox should match the requested dimensions")
	}
}

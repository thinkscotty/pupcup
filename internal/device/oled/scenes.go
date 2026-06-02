package oled

import (
	"fmt"
	"time"
)

// drawCenteredText centers a string horizontally at the panel's vertical mid.
func drawCenteredText(m *mono, msg string) {
	msg = upper(msg)
	w := textWidth(msg)
	x := (Width - w) / 2
	if x < 0 {
		x = 0
	}
	y := (Height - 7) / 2
	drawText(m, x, y, msg)
}

// drawDogSelector draws a centered, scale-2 dog name with an Index/Total
// indicator on the bottom-right.
func drawDogSelector(m *mono, sc DogSelectorScene) {
	name := upper(sc.Dog.Name)
	scale := 2
	w := textWidth(name) * scale
	if w > Width-4 {
		// Fall back to scale 1 if it would overflow.
		scale = 1
		w = textWidth(name)
	}
	x := (Width - w) / 2
	if x < 0 {
		x = 0
	}
	y := (Height - 7*scale) / 2
	drawTextScaled(m, x, y, name, scale)

	if sc.Total > 1 {
		idx := fmt.Sprintf("%d/%d", sc.Index+1, sc.Total)
		idxW := textWidth(idx)
		drawText(m, Width-idxW-2, Height-9, idx)
	}

	// Subtle "ROTATE" hint on bottom-left.
	drawText(m, 2, Height-9, "TURN")
}

// drawSummary draws the locked-summary scene: each dog's name + score,
// with a countdown to lock expiry on the bottom row.
func drawSummary(m *mono, sc LockedSummaryScene) {
	drawText(m, 2, 2, "FED")
	hdr := "TODAY"
	drawText(m, Width-textWidth(hdr)-2, 2, hdr)

	y := 14
	for _, e := range sc.Entries {
		// Score badge at the left: G/Y/R single-char; full-block if snack added.
		var badge string
		switch e.Score {
		case "full":
			badge = "G"
		case "partial":
			badge = "Y"
		case "none":
			badge = "R"
		default:
			badge = "-"
		}
		// Filled square around the badge for emphasis.
		m.fillRect(2, y-1, 11, y+8, true)
		// Punch-out the letter (XOR via redraw — simpler: turn pixels off).
		// We just draw the letter on top with inverted bits by clearing then setting.
		// Easier: clear box, draw letter normally.
		m.fillRect(2, y-1, 11, y+8, false)
		drawText(m, 4, y, badge)

		name := upper(e.DogName)
		drawText(m, 16, y, name)

		if e.HasSnack {
			drawText(m, Width-8, y, "*")
		}
		y += 11
		if y > Height-10 {
			break
		}
	}

	if !sc.LockedUntil.IsZero() && !sc.Now.IsZero() && sc.LockedUntil.After(sc.Now) {
		rem := sc.LockedUntil.Sub(sc.Now).Round(time.Minute)
		drawText(m, 2, Height-9, "UNLOCK "+formatDuration(rem))
	}
}

// drawSnack draws snack-mode UI: highlighted dog name, snack label, idle countdown.
func drawSnack(m *mono, sc SnackModeScene) {
	hdr := "SNACK"
	drawText(m, (Width-textWidth(hdr))/2, 4, hdr)

	name := upper(sc.Dog.Name)
	scale := 2
	w := textWidth(name) * scale
	if w > Width-4 {
		scale = 1
		w = textWidth(name)
	}
	x := (Width - w) / 2
	drawTextScaled(m, x, 22, name, scale)

	if sc.Remaining > 0 {
		s := fmt.Sprintf("%dS LEFT", int(sc.Remaining.Round(time.Second).Seconds()))
		drawText(m, (Width-textWidth(s))/2, Height-9, s)
	}
}

// formatDuration formats a duration as H:MM or M:SS.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%d:%02dH", h, m)
	}
	mins := int(d.Minutes())
	if mins >= 1 {
		return fmt.Sprintf("%dM", mins)
	}
	return fmt.Sprintf("%dS", int(d.Seconds()))
}

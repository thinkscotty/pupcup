package gc9a01

import (
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/domain"
)

func TestRGB565(t *testing.T) {
	cases := []struct {
		name    string
		r, g, b uint8
		hi, lo  byte
	}{
		{"red", 255, 0, 0, 0xF8, 0x00},
		{"green", 0, 255, 0, 0x07, 0xE0},
		{"blue", 0, 0, 255, 0x00, 0x1F},
		{"white", 255, 255, 255, 0xFF, 0xFF},
		{"black", 0, 0, 0, 0x00, 0x00},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hi, lo := rgb565(c.r, c.g, c.b)
			if hi != c.hi || lo != c.lo {
				t.Fatalf("rgb565(%d,%d,%d) = %#02x %#02x, want %#02x %#02x",
					c.r, c.g, c.b, hi, lo, c.hi, c.lo)
			}
		})
	}
}

func TestFakeRecordsFill(t *testing.T) {
	f := NewFake()
	if err := f.FillRGB(10, 20, 30); err != nil {
		t.Fatalf("FillRGB: %v", err)
	}
	if r, g, b := f.LastFill(); r != 10 || g != 20 || b != 30 {
		t.Fatalf("LastFill = %d,%d,%d, want 10,20,30", r, g, b)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := f.FillRGB(1, 2, 3); err == nil {
		t.Fatal("FillRGB after Close: want error, got nil")
	}
}

// TestColorFrameRendersScenes checks every scene type paints something onto the
// canvas (exercises the layout/font/RGB565 path on the laptop, with no device).
func TestColorFrameRendersScenes(t *testing.T) {
	now := time.Now()
	scenes := []display.Scene{
		display.SplashScene{Message: "PUPCUP", Now: now},
		display.DogSelectorScene{Dog: domain.Dog{Name: "Cleo", AccentColor: "#A8D8B9"}, Index: 0, Total: 3, Now: now},
		display.LockedSummaryScene{
			Entries: []display.SummaryEntry{
				{DogName: "Cleo", Score: domain.ScoreFull},
				{DogName: "Rio", Score: domain.ScorePartial, HasSnack: true},
			},
			LockedUntil: now.Add(3*time.Hour + 42*time.Minute),
			Now:         now,
		},
		display.SnackModeScene{Dog: domain.Dog{Name: "Otis"}, Remaining: 45 * time.Second},
		display.AddInSelectScene{
			Dog:     domain.Dog{Name: "Pip"},
			Score:   domain.ScoreFull,
			Choices: []display.AddInChoice{{Label: "Chicken"}, {Label: "Other", IsOther: true}},
			Index:   1,
		},
	}
	for _, s := range scenes {
		c := colorFrame(s)
		if len(c.pix) != Width*Height*2 {
			t.Fatalf("%T: pix len = %d, want %d", s, len(c.pix), Width*Height*2)
		}
		nonBg := false
		for _, b := range c.pix {
			if b != 0 {
				nonBg = true
				break
			}
		}
		if !nonBg {
			t.Errorf("%T: canvas is entirely background; nothing drawn", s)
		}
	}
}

// TestParseAccent confirms hex parsing and the white fallback.
func TestParseAccent(t *testing.T) {
	if got := accentColor("#FF0000"); got != color565(255, 0, 0) {
		t.Errorf("accentColor(#FF0000) = %v, want red", got)
	}
	if got := accentColor("nonsense"); got != colFg {
		t.Errorf("accentColor(nonsense) = %v, want white fallback", got)
	}
}

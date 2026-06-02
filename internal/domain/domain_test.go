package domain

import (
	"testing"
	"time"
)

func TestButtonColor_MealScore(t *testing.T) {
	cases := []struct {
		c    ButtonColor
		want Score
		ok   bool
	}{
		{BtnGreen, ScoreFull, true},
		{BtnYellow, ScorePartial, true},
		{BtnRed, ScoreNone, true},
		{BtnBlue, "", false},
	}
	for _, tc := range cases {
		got, ok := tc.c.MealScore()
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.c, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDeviceLock_IsLocked(t *testing.T) {
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	if (DeviceLock{}).IsLocked(now) {
		t.Error("nil Until should not be locked")
	}
	if !(DeviceLock{Until: &future}).IsLocked(now) {
		t.Error("future Until should be locked")
	}
	if (DeviceLock{Until: &past}).IsLocked(now) {
		t.Error("past Until should not be locked")
	}
}

func TestFeeding_Validate(t *testing.T) {
	good := Feeding{DogID: 1, TS: time.Now(), Score: ScoreFull, Kind: FeedStandard, Source: SourceButton}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid feeding rejected: %v", err)
	}
	bad := good
	bad.Score = "weird"
	if err := bad.Validate(); err == nil {
		t.Error("invalid score accepted")
	}
}

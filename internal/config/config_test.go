package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault_IsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	if cfg.Location == nil {
		t.Fatal("Location not resolved")
	}
}

func TestLoad_FileOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen: \":8080\"\nmeal_lock_minutes: 60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want :8080", cfg.Listen)
	}
	if cfg.MealLockMinutes != 60 {
		t.Errorf("MealLockMinutes = %d, want 60", cfg.MealLockMinutes)
	}
	// Defaults remain for unspecified fields.
	if cfg.NeopixelCount != 8 {
		t.Errorf("NeopixelCount = %d, want default 8", cfg.NeopixelCount)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("PUPCUP_LISTEN", ":9090")
	t.Setenv("PUPCUP_BUTTON_PINS_GREEN", "13") // a free BCM pin (avoids the LCD DC/RST defaults)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want :9090", cfg.Listen)
	}
	if cfg.ButtonPins.Green != 13 {
		t.Errorf("ButtonPins.Green = %d, want 13", cfg.ButtonPins.Green)
	}
}

func TestValidate_DuplicatePinsRejected(t *testing.T) {
	cfg := Default()
	cfg.ButtonPins.Green = cfg.RotaryPins.CLK // collision
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "share pin") {
		t.Fatalf("expected duplicate pin error, got %v", err)
	}
}

func TestValidate_BadTimezoneRejected(t *testing.T) {
	cfg := Default()
	cfg.Timezone = "Not/A/Zone"
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "timezone") {
		t.Fatalf("expected timezone error, got %v", err)
	}
}

func TestValidate_BadDisplayRejected(t *testing.T) {
	cfg := Default()
	cfg.Display = "epaper"
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "display") {
		t.Fatalf("expected display error, got %v", err)
	}
}

func TestValidate_OLEDDisplayIgnoresLCDPins(t *testing.T) {
	cfg := Default()
	cfg.Display = "oled"
	// With the OLED selected the LCD GPIO lines are unused, so leaving them
	// unset (0) — which would fail the >0 pin check if validated — must be OK.
	cfg.LCDDCPin = 0
	cfg.LCDRSTPin = 0
	cfg.LCDSPIDevice = ""
	if err := cfg.validate(); err != nil {
		t.Fatalf("oled config with empty LCD fields should be valid: %v", err)
	}
}

func TestValidate_LCDPinCollisionRejected(t *testing.T) {
	cfg := Default()
	cfg.LCDDCPin = cfg.ButtonPins.Green // collide DC with a button pin
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "share pin") {
		t.Fatalf("expected duplicate pin error, got %v", err)
	}
}

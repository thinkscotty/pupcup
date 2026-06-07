// Package config defines the PupCup configuration: YAML on disk, env-var
// overrides, validated at load time. One Config struct, one Load function.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen   string `yaml:"listen" env:"LISTEN"`
	DBPath   string `yaml:"db_path" env:"DB_PATH"`
	PhotoDir string `yaml:"photo_dir" env:"PHOTO_DIR"`
	Timezone string `yaml:"timezone" env:"TIMEZONE"`

	// Hardware
	Display       string     `yaml:"display" env:"DISPLAY"` // "gc9a01" (round SPI LCD) | "oled" (SSD1306 I2C)
	SPIDevice     string     `yaml:"spi_device" env:"SPI_DEVICE"`
	I2CBus        int        `yaml:"i2c_bus" env:"I2C_BUS"`
	OLEDAddr      uint16     `yaml:"oled_addr" env:"OLED_ADDR"`
	LCDSPIDevice  string     `yaml:"lcd_spi_device" env:"LCD_SPI_DEVICE"` // GC9A01 on SPI1
	LCDDCPin      int        `yaml:"lcd_dc_pin" env:"LCD_DC_PIN"`         // data/command select (BCM)
	LCDRSTPin     int        `yaml:"lcd_rst_pin" env:"LCD_RST_PIN"`       // reset (BCM)
	LCDRotation   int        `yaml:"lcd_rotation" env:"LCD_ROTATION"`     // GC9A01 screen rotation, degrees clockwise: 0/90/180/270
	NeopixelCount int        `yaml:"neopixel_count" env:"NEOPIXEL_COUNT"`
	ButtonPins    ButtonPins `yaml:"button_pins"`
	RotaryPins    RotaryPins `yaml:"rotary_pins"`

	ButtonDebounceMS int `yaml:"button_debounce_ms" env:"BUTTON_DEBOUNCE_MS"`
	RotaryDebounceMS int `yaml:"rotary_debounce_ms" env:"ROTARY_DEBOUNCE_MS"`
	LongPressMS      int `yaml:"long_press_ms" env:"LONG_PRESS_MS"`

	// Behavior
	MealLockMinutes          int    `yaml:"meal_lock_minutes" env:"MEAL_LOCK_MINUTES"`
	MealCompleteGraceMinutes int    `yaml:"meal_complete_grace_minutes" env:"MEAL_COMPLETE_GRACE_MINUTES"`
	SnackModeIdleSeconds     int    `yaml:"snack_mode_idle_seconds" env:"SNACK_MODE_IDLE_SECONDS"`
	AddInIdleSeconds         int    `yaml:"addin_idle_seconds" env:"ADDIN_IDLE_SECONDS"`
	DefaultFeedKind          string `yaml:"default_feed_kind" env:"DEFAULT_FEED_KIND"`
	MDNSHostname             string `yaml:"mdns_hostname" env:"MDNS_HOSTNAME"`

	// Web / photos
	PhotoMaxKB int `yaml:"photo_max_kb" env:"PHOTO_MAX_KB"`
	PhotoMaxPx int `yaml:"photo_max_px" env:"PHOTO_MAX_PX"`

	// Resolved at validation
	Location *time.Location `yaml:"-"`
}

type ButtonPins struct {
	Green  int `yaml:"green"  env:"BUTTON_PINS_GREEN"`
	Yellow int `yaml:"yellow" env:"BUTTON_PINS_YELLOW"`
	Red    int `yaml:"red"    env:"BUTTON_PINS_RED"`
	Blue   int `yaml:"blue"   env:"BUTTON_PINS_BLUE"`
}

type RotaryPins struct {
	CLK int `yaml:"clk" env:"ROTARY_PINS_CLK"`
	DT  int `yaml:"dt"  env:"ROTARY_PINS_DT"`
	SW  int `yaml:"sw"  env:"ROTARY_PINS_SW"`
}

// Default returns a config populated with the v1 ship defaults.
func Default() Config {
	return Config{
		Listen:   ":80",
		DBPath:   "/var/lib/pupcup/pupcup.sqlite",
		PhotoDir: "/var/lib/pupcup/photos",
		Timezone: "America/New_York",

		Display:       "gc9a01",
		SPIDevice:     "/dev/spidev0.0",
		I2CBus:        1,
		OLEDAddr:      0x3C,
		LCDSPIDevice:  "/dev/spidev1.0",
		LCDDCPin:      25,
		LCDRSTPin:     24,
		NeopixelCount: 8,
		ButtonPins:    ButtonPins{Green: 12, Yellow: 16, Red: 5, Blue: 6},
		RotaryPins:    RotaryPins{CLK: 17, DT: 27, SW: 22},

		ButtonDebounceMS: 25,
		RotaryDebounceMS: 5,
		LongPressMS:      1500,

		MealLockMinutes:          240,
		MealCompleteGraceMinutes: 15,
		SnackModeIdleSeconds:     60,
		AddInIdleSeconds:         30,
		DefaultFeedKind:          "standard",
		MDNSHostname:             "pupcup",

		PhotoMaxKB: 150,
		PhotoMaxPx: 320,
	}
}

// Load reads a YAML config file, applies env-var overrides (PUPCUP_* prefix),
// and validates the result. An empty path uses defaults + env overrides only.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if err := applyEnvOverrides(&cfg, "PUPCUP_"); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyEnvOverrides walks struct fields with `env:"NAME"` tags and applies
// values from os.Getenv(prefix + NAME) when set. Recurses into struct fields.
func applyEnvOverrides(v any, prefix string) error {
	rv := reflect.ValueOf(v).Elem()
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		fv := rv.Field(i)
		ft := rt.Field(i)
		if fv.Kind() == reflect.Struct {
			if err := applyEnvOverrides(fv.Addr().Interface(), prefix); err != nil {
				return err
			}
			continue
		}
		name := ft.Tag.Get("env")
		if name == "" {
			continue
		}
		raw, ok := os.LookupEnv(prefix + name)
		if !ok {
			continue
		}
		if err := setFromString(fv, raw); err != nil {
			return fmt.Errorf("env %s%s: %w", prefix, name, err)
		}
	}
	return nil
}

func setFromString(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 0, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 0, 64)
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	default:
		return fmt.Errorf("unsupported kind %s", fv.Kind())
	}
	return nil
}

func (c *Config) validate() error {
	var errs []string

	if c.Listen == "" {
		errs = append(errs, "listen is required")
	}
	if c.DBPath == "" {
		errs = append(errs, "db_path is required")
	}
	if c.PhotoDir == "" {
		errs = append(errs, "photo_dir is required")
	}
	if c.Timezone == "" {
		errs = append(errs, "timezone is required")
	} else {
		loc, err := time.LoadLocation(c.Timezone)
		if err != nil {
			errs = append(errs, fmt.Sprintf("timezone %q: %v", c.Timezone, err))
		} else {
			c.Location = loc
		}
	}

	if c.NeopixelCount < 1 {
		errs = append(errs, "neopixel_count must be >= 1")
	}
	if c.MealLockMinutes < 1 {
		errs = append(errs, "meal_lock_minutes must be >= 1")
	}
	if c.MealCompleteGraceMinutes < 0 {
		// 0 = lock immediately on the last recorded meal even if dogs remain un-fed.
		errs = append(errs, "meal_complete_grace_minutes must be >= 0")
	}
	if c.SnackModeIdleSeconds < 1 {
		errs = append(errs, "snack_mode_idle_seconds must be >= 1")
	}
	if c.AddInIdleSeconds < 1 {
		errs = append(errs, "addin_idle_seconds must be >= 1")
	}
	if c.PhotoMaxKB < 1 {
		errs = append(errs, "photo_max_kb must be >= 1")
	}
	if c.PhotoMaxPx < 1 {
		errs = append(errs, "photo_max_px must be >= 1")
	}
	if c.ButtonDebounceMS < 0 {
		errs = append(errs, "button_debounce_ms must be >= 0")
	}
	if c.RotaryDebounceMS < 0 {
		errs = append(errs, "rotary_debounce_ms must be >= 0")
	}
	if c.LongPressMS < 100 {
		errs = append(errs, "long_press_ms must be >= 100")
	}

	pins := map[string]int{
		"button_pins.green":  c.ButtonPins.Green,
		"button_pins.yellow": c.ButtonPins.Yellow,
		"button_pins.red":    c.ButtonPins.Red,
		"button_pins.blue":   c.ButtonPins.Blue,
		"rotary_pins.clk":    c.RotaryPins.CLK,
		"rotary_pins.dt":     c.RotaryPins.DT,
		"rotary_pins.sw":     c.RotaryPins.SW,
	}

	// The active display selects which extra hardware lines must be valid. The
	// GC9A01 drives two plain GPIO lines (DC, RST) that must be >0 and not
	// collide with the button/rotary pins, so fold them into the uniqueness
	// check below. The OLED needs only its I2C bus/addr (validated implicitly).
	switch c.Display {
	case "oled":
		// SSD1306 over I2C; the LCD SPI/GPIO lines are unused.
	case "gc9a01":
		if c.LCDSPIDevice == "" {
			errs = append(errs, "lcd_spi_device is required when display is gc9a01")
		}
		switch c.LCDRotation {
		case 0, 90, 180, 270:
		default:
			errs = append(errs, fmt.Sprintf("lcd_rotation %d must be 0, 90, 180, or 270", c.LCDRotation))
		}
		pins["lcd_dc_pin"] = c.LCDDCPin
		pins["lcd_rst_pin"] = c.LCDRSTPin
	default:
		errs = append(errs, fmt.Sprintf("display %q must be oled or gc9a01", c.Display))
	}

	seen := map[int]string{}
	for name, pin := range pins {
		if pin <= 0 {
			errs = append(errs, fmt.Sprintf("%s must be > 0", name))
			continue
		}
		if other, dup := seen[pin]; dup {
			errs = append(errs, fmt.Sprintf("%s and %s share pin %d", name, other, pin))
		}
		seen[pin] = name
	}

	switch c.DefaultFeedKind {
	case "standard", "nonstandard":
	default:
		errs = append(errs, fmt.Sprintf("default_feed_kind %q must be standard or nonstandard", c.DefaultFeedKind))
	}

	if c.MDNSHostname == "" {
		errs = append(errs, "mdns_hostname is required")
	}

	if len(errs) > 0 {
		return errors.New("invalid config:\n  - " + strings.Join(errs, "\n  - "))
	}
	return nil
}

// MealLock returns the configured post-meal lock duration.
func (c Config) MealLock() time.Duration {
	return time.Duration(c.MealLockMinutes) * time.Minute
}

// MealCompleteGrace returns how long, after the first dog is fed, the device
// waits for the remaining dogs before locking with a partial meal. 0 = lock as
// soon as the next tick observes the meal is still incomplete.
func (c Config) MealCompleteGrace() time.Duration {
	return time.Duration(c.MealCompleteGraceMinutes) * time.Minute
}

// SnackIdle returns the configured snack-mode idle timeout.
func (c Config) SnackIdle() time.Duration {
	return time.Duration(c.SnackModeIdleSeconds) * time.Second
}

// AddInIdle returns the AddInSelect walk-away timeout: when it elapses the
// pending meal is committed untagged rather than lost.
func (c Config) AddInIdle() time.Duration {
	return time.Duration(c.AddInIdleSeconds) * time.Second
}

// LongPress returns the long-press threshold.
func (c Config) LongPress() time.Duration {
	return time.Duration(c.LongPressMS) * time.Millisecond
}

// ButtonDebounce returns the per-button debounce quiet period.
func (c Config) ButtonDebounce() time.Duration {
	return time.Duration(c.ButtonDebounceMS) * time.Millisecond
}

// RotaryDebounce returns the rotary debounce window. Retained for config
// compatibility; the Buxton decoder rejects bounce structurally and ignores it.
func (c Config) RotaryDebounce() time.Duration {
	return time.Duration(c.RotaryDebounceMS) * time.Millisecond
}

// AbsDBPath returns DBPath made absolute relative to cwd if it is relative.
func (c Config) AbsDBPath() (string, error) {
	if filepath.IsAbs(c.DBPath) {
		return c.DBPath, nil
	}
	return filepath.Abs(c.DBPath)
}

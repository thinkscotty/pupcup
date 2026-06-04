// Package oled drives the SSD1306 128x64 monochrome OLED. It is one
// implementation of display.Renderer: it owns the I2C device and the 1-bpp
// framebuffer, and renders the shared display.Scene view-models in monochrome.
// The scene contract and the Fake live in internal/device/display; the glyphs
// live in internal/device/font.
package oled

import (
	"image"

	"github.com/scottyturner/pupcup/internal/device/display"
)

// Width and height of the supported SSD1306 panel in pixels.
const (
	Width  = 128
	Height = 64
)

// frame builds the 1-bpp framebuffer for a scene. The Renderer implementation
// consumes the resulting image.Image and writes it to the SSD1306.
func frame(s display.Scene) image.Image {
	img := newMono(Width, Height)
	switch sc := s.(type) {
	case display.SplashScene:
		drawCenteredText(img, sc.Message)
	case display.DogSelectorScene:
		drawDogSelector(img, sc)
	case display.LockedSummaryScene:
		drawSummary(img, sc)
	case display.SnackModeScene:
		drawSnack(img, sc)
	case display.AddInSelectScene:
		drawAddInSelect(img, sc)
	}
	return img
}

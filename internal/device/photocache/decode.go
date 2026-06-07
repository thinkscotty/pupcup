package photocache

import (
	"image"
	_ "image/jpeg" // register JPEG decoder (a format the web uploader accepts)
	_ "image/png"  // register PNG decoder
	"io"

	xdraw "golang.org/x/image/draw"
)

// decodeAvatar decodes an image from r, crops it to a centered square, and
// resamples it to size×size RGBA with a high-quality filter. The result is the
// square ui.DrawAvatar expects: it clips this to a circle for the dog avatar.
//
// Only JPEG and PNG are handled (the formats the web uploader stores); anything
// else surfaces as a decode error. Non-square sources lose their longer edges to
// the centered crop — the standard trade for a circular avatar.
func decodeAvatar(r io.Reader, size int) (*image.RGBA, error) {
	src, _, err := image.Decode(r)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	// Centered square crop region, in source coordinates.
	sx := b.Min.X + (b.Dx()-side)/2
	sy := b.Min.Y + (b.Dy()-side)/2
	crop := image.Rect(sx, sy, sx+side, sy+side)

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Src, nil)
	return dst, nil
}

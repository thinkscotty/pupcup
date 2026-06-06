package anim

import "image"

// Rect is a half-open pixel rectangle [X0,X1) x [Y0,Y1) in framebuffer
// coordinates. It matches the convention the GC9A01 driver's FlushRect expects,
// so a Rect produced here can be flushed without translation.
type Rect struct{ X0, Y0, X1, Y1 int }

// Empty reports whether r covers no pixels.
func (r Rect) Empty() bool { return r.X0 >= r.X1 || r.Y0 >= r.Y1 }

// Width returns the rectangle's pixel width (0 if empty).
func (r Rect) Width() int {
	if r.Empty() {
		return 0
	}
	return r.X1 - r.X0
}

// Height returns the rectangle's pixel height (0 if empty).
func (r Rect) Height() int {
	if r.Empty() {
		return 0
	}
	return r.Y1 - r.Y0
}

// Union returns the smallest rectangle containing both r and o, treating an
// empty operand as "nothing" so the other is returned unchanged.
func (r Rect) Union(o Rect) Rect {
	if r.Empty() {
		return o
	}
	if o.Empty() {
		return r
	}
	return Rect{min(r.X0, o.X0), min(r.Y0, o.Y0), max(r.X1, o.X1), max(r.Y1, o.Y1)}
}

// Clamp returns r intersected with the [0,w) x [0,h) panel bounds.
func (r Rect) Clamp(w, h int) Rect {
	if r.X0 < 0 {
		r.X0 = 0
	}
	if r.Y0 < 0 {
		r.Y0 = 0
	}
	if r.X1 > w {
		r.X1 = w
	}
	if r.Y1 > h {
		r.Y1 = h
	}
	return r
}

// unionAll merges rs into a single bounding box. The result is empty when rs is
// empty or every element is empty.
func unionAll(rs []Rect) Rect {
	var b Rect
	for _, r := range rs {
		b = b.Union(r)
	}
	return b
}

// PackRGBARect converts the half-open sub-rectangle r of src (an 8-bit RGBA
// image) into big-endian RGB565 and writes it into dst at matching full-frame
// offsets, where dst is a full-frame buffer whose row stride is src width * 2
// bytes. It reuses the exact bit layout of the GC9A01 driver's rgb565 — high
// byte first — so packed bytes are panel-ready, and it allocates nothing.
//
// r must lie within src.Bounds(); the caller clamps first (the engine does).
// Pixels of dst outside r are left untouched, which is what makes a pooled
// buffer reusable across frames: only the flushed rectangle's bytes matter.
func PackRGBARect(dst []byte, src *image.RGBA, r Rect) {
	w := src.Rect.Dx()
	for y := r.Y0; y < r.Y1; y++ {
		si := src.PixOffset(r.X0, y)
		di := (y*w + r.X0) * 2
		for x := r.X0; x < r.X1; x++ {
			cr := src.Pix[si]
			cg := src.Pix[si+1]
			cb := src.Pix[si+2]
			v := (uint16(cr&0xF8) << 8) | (uint16(cg&0xFC) << 3) | (uint16(cb) >> 3)
			dst[di] = byte(v >> 8)
			dst[di+1] = byte(v)
			si += 4
			di += 2
		}
	}
}

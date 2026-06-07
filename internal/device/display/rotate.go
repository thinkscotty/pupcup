package display

// Rotation is a clockwise screen rotation applied at the flush boundary. The
// animation engine and every scene keep drawing in the panel's native upright
// 240×240 space; the rotation only re-maps pixels on their way to the glass, so a
// panel mounted at a non-standard angle in the case still reads upright. It is a
// GC9A01 concern (the only RectFlusher panel) and is additive: Rot0 is a no-op.
type Rotation uint8

const (
	Rot0   Rotation = iota // no rotation (standard mount)
	Rot90                  // 90° clockwise
	Rot180                 // upside-down
	Rot270                 // 270° clockwise == 90° counter-clockwise
)

// RotationFromDegrees converts a clockwise angle in degrees to a Rotation. Only
// the four right angles are valid; ok is false otherwise so callers can default
// to Rot0 and warn.
func RotationFromDegrees(deg int) (r Rotation, ok bool) {
	switch deg {
	case 0:
		return Rot0, true
	case 90:
		return Rot90, true
	case 180:
		return Rot180, true
	case 270:
		return Rot270, true
	default:
		return Rot0, false
	}
}

// RotateRGB565 copies the half-open logical sub-rectangle [x0,x1) x [y0,y1) of
// src into dst, applying rot, and returns the rectangle the pixels land in on the
// panel. Both src and dst are full-frame square n×n RGB565 buffers (2 bytes per
// pixel, row stride n*2); only the destination rectangle's bytes are written, so
// a reused dst stays valid for the next flush exactly like the un-rotated path.
//
// Rotation is a lossless permutation of 2-byte pixels: the panel byte order
// (big-endian RGB565) is preserved, so colors are untouched (the MADCTL BGR fix
// stays the single source of truth). The caller clamps the rect to the panel
// first, as the engine already does for its dirty box.
func RotateRGB565(dst, src []byte, n, x0, y0, x1, y1 int, rot Rotation) (px0, py0, px1, py1 int) {
	switch rot {
	case Rot90: // px = n-1-ly, py = lx
		px0, py0, px1, py1 = n-y1, x0, n-y0, x1
		for py := py0; py < py1; py++ {
			lx := py
			di := (py*n + px0) * 2
			for px := px0; px < px1; px++ {
				si := ((n-1-px)*n + lx) * 2
				dst[di] = src[si]
				dst[di+1] = src[si+1]
				di += 2
			}
		}
	case Rot180: // px = n-1-lx, py = n-1-ly
		px0, py0, px1, py1 = n-x1, n-y1, n-x0, n-y0
		for py := py0; py < py1; py++ {
			ly := n - 1 - py
			di := (py*n + px0) * 2
			for px := px0; px < px1; px++ {
				si := (ly*n + (n - 1 - px)) * 2
				dst[di] = src[si]
				dst[di+1] = src[si+1]
				di += 2
			}
		}
	case Rot270: // px = ly, py = n-1-lx
		px0, py0, px1, py1 = y0, n-x1, y1, n-x0
		for py := py0; py < py1; py++ {
			lx := n - 1 - py
			di := (py*n + px0) * 2
			for px := px0; px < px1; px++ {
				si := (px*n + lx) * 2
				dst[di] = src[si]
				dst[di+1] = src[si+1]
				di += 2
			}
		}
	default: // Rot0 — straight per-row copy
		px0, py0, px1, py1 = x0, y0, x1, y1
		for py := y0; py < y1; py++ {
			off := (py*n + x0) * 2
			copy(dst[off:(py*n+x1)*2], src[off:(py*n+x1)*2])
		}
	}
	return px0, py0, px1, py1
}

// rotatedFlusher wraps a RectFlusher and rotates every blit before forwarding it.
// It owns a single reused panel-order scratch buffer; the animation engine's flush
// goroutine is the sole caller of FlushRect, so no synchronization is needed.
type rotatedFlusher struct {
	inner RectFlusher
	rot   Rotation
	n     int
	buf   []byte // n*n*2 panel-order scratch, reused across flushes
}

// NewRotatedFlusher returns a RectFlusher that rotates each blit by rot before
// forwarding it to inner. For Rot0 it returns inner unchanged (zero overhead); n
// is the square panel dimension. The result is single-goroutine, matching the
// engine's dedicated flush goroutine.
func NewRotatedFlusher(inner RectFlusher, n int, rot Rotation) RectFlusher {
	if rot == Rot0 {
		return inner
	}
	return &rotatedFlusher{inner: inner, rot: rot, n: n, buf: make([]byte, n*n*2)}
}

func (f *rotatedFlusher) FlushRect(buf []byte, x0, y0, x1, y1 int) error {
	px0, py0, px1, py1 := RotateRGB565(f.buf, buf, f.n, x0, y0, x1, y1, f.rot)
	return f.inner.FlushRect(f.buf, px0, py0, px1, py1)
}

var _ RectFlusher = (*rotatedFlusher)(nil)

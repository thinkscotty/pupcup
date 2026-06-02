package neopixel

import (
	"bytes"
	"testing"
)

// TestEncodeFrame_Patterns spot-checks the WS-bit-to-SPI-triplet encoding
// against pre-computed expected bytes for a known input.
func TestEncodeFrame_Patterns(t *testing.T) {
	// Single LED, all-zero color: 24 bits of 0 => 24 triplets of 0b100 =>
	// pattern: 100 100 100 100 100 100 100 100 = (binary)
	// 100100 10 / 0100100 1 / 00100100 / 100100 10 / ...
	// Easier: just compute by feeding back through encodeFrame and verify
	// length + that no triplet violates the expected MSB pattern.
	dst := make([]byte, encodeBitsPerLED+resetBytes)
	encodeFrame(dst, []Color{{}})
	// Check that the first 9 bytes only have bit-pattern '100' triplets.
	// Pull bits MSB-first and verify every 3-bit group is 0b100.
	for i := 0; i < 24; i++ {
		bitOff := i * 3
		var triplet uint8
		for j := 0; j < 3; j++ {
			b := dst[(bitOff+j)/8]
			triplet = (triplet << 1) | ((b >> uint(7-(bitOff+j)%8)) & 1)
		}
		if triplet != 0b100 {
			t.Fatalf("zero-bit triplet at bit %d = %03b, want 100", i, triplet)
		}
	}
	// The reset bytes must be untouched (zero).
	if !bytes.Equal(dst[encodeBitsPerLED:], make([]byte, resetBytes)) {
		t.Fatal("reset bytes were modified")
	}
}

func TestEncodeFrame_OneBit(t *testing.T) {
	// All-ones color (R=G=B=0xFF): every triplet should be 0b110.
	dst := make([]byte, encodeBitsPerLED+resetBytes)
	encodeFrame(dst, []Color{{R: 0xFF, G: 0xFF, B: 0xFF}})
	for i := 0; i < 24; i++ {
		bitOff := i * 3
		var triplet uint8
		for j := 0; j < 3; j++ {
			b := dst[(bitOff+j)/8]
			triplet = (triplet << 1) | ((b >> uint(7-(bitOff+j)%8)) & 1)
		}
		if triplet != 0b110 {
			t.Fatalf("one-bit triplet at bit %d = %03b, want 110", i, triplet)
		}
	}
}

func TestEncodeFrame_GRBOrder(t *testing.T) {
	// G=1 R=0 B=0 means the very first 8 bits transmitted must encode G=1.
	// Specifically, G is binary 00000001. The first 7 bits are zeros (triplet
	// 100), the 8th bit is 1 (triplet 110). Decode and verify.
	dst := make([]byte, encodeBitsPerLED+resetBytes)
	encodeFrame(dst, []Color{{R: 0, G: 1, B: 0}})
	for i := 0; i < 24; i++ {
		bitOff := i * 3
		var triplet uint8
		for j := 0; j < 3; j++ {
			b := dst[(bitOff+j)/8]
			triplet = (triplet << 1) | ((b >> uint(7-(bitOff+j)%8)) & 1)
		}
		want := uint8(0b100)
		if i == 7 { // G's LSB transmitted last in MSB-first.
			want = 0b110
		}
		if triplet != want {
			t.Fatalf("bit %d = %03b, want %03b", i, triplet, want)
		}
	}
}

func TestFakeRecordsFrames(t *testing.T) {
	f := NewFake(3)
	if err := f.SetPixel(1, Color{R: 1}); err != nil {
		t.Fatal(err)
	}
	if err := f.Show(); err != nil {
		t.Fatal(err)
	}
	if f.FrameCount() != 1 {
		t.Fatalf("frame count: %d", f.FrameCount())
	}
	last := f.LastFrame()
	if last[1].R != 1 || last[0].R != 0 {
		t.Fatalf("frame: %+v", last)
	}
}

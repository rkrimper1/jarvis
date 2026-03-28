package faceanalysis

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// ── newHUDPalette ─────────────────────────────────────────────────────

func TestNewHUDPalette_FullOpacity(t *testing.T) {
	p := newHUDPalette(1.0)
	if p.cyan.A != 0xff {
		t.Errorf("cyan.A = 0x%02x, want 0xff", p.cyan.A)
	}
	if p.cyanGlow.A != 0x88 {
		t.Errorf("cyanGlow.A = 0x%02x, want 0x88", p.cyanGlow.A)
	}
	if p.cyanFill.A != 0x40 {
		t.Errorf("cyanFill.A = 0x%02x, want 0x40", p.cyanFill.A)
	}
	if p.cyanDim.A != 0xcc {
		t.Errorf("cyanDim.A = 0x%02x, want 0xcc", p.cyanDim.A)
	}
}

func TestNewHUDPalette_ZeroOpacityDefaultsToFull(t *testing.T) {
	// 0 is invalid; should treat as 1.0
	p := newHUDPalette(0)
	if p.cyan.A != 0xff {
		t.Errorf("zero opacity: cyan.A = 0x%02x, want 0xff (defaulted to 1.0)", p.cyan.A)
	}
}

func TestNewHUDPalette_NegativeOpacityDefaultsToFull(t *testing.T) {
	p := newHUDPalette(-0.5)
	if p.cyan.A != 0xff {
		t.Errorf("negative opacity: cyan.A = 0x%02x, want 0xff (defaulted to 1.0)", p.cyan.A)
	}
}

func TestNewHUDPalette_HalfOpacity(t *testing.T) {
	p := newHUDPalette(0.5)
	scaleAlpha := func(a uint8, opacity float64) uint8 { return uint8(float64(a) * opacity) }
	if p.cyan.A != scaleAlpha(0xff, 0.5) {
		t.Errorf("half opacity: cyan.A = %d, want %d", p.cyan.A, scaleAlpha(0xff, 0.5))
	}
	if p.cyanGlow.A != scaleAlpha(0x88, 0.5) {
		t.Errorf("half opacity: cyanGlow.A = %d, want %d", p.cyanGlow.A, scaleAlpha(0x88, 0.5))
	}
}

func TestNewHUDPalette_HueIsAlwaysCyan(t *testing.T) {
	for _, opacity := range []float64{0.1, 0.5, 1.0} {
		p := newHUDPalette(opacity)
		for name, c := range map[string]color.NRGBA{
			"cyan":     p.cyan,
			"cyanGlow": p.cyanGlow,
			"cyanFill": p.cyanFill,
			"cyanDim":  p.cyanDim,
		} {
			if c.R != 0x00 || c.G != 0xd4 || c.B != 0xff {
				t.Errorf("opacity %.1f %s: RGB = (%d,%d,%d), want (0,212,255)",
					opacity, name, c.R, c.G, c.B)
			}
		}
	}
}

// ── toGrayscale ───────────────────────────────────────────────────────

func TestToGrayscale_OutputDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 20))
	pixels, cols, rows := toGrayscale(img)
	if cols != 10 {
		t.Errorf("cols = %d, want 10", cols)
	}
	if rows != 20 {
		t.Errorf("rows = %d, want 20", rows)
	}
	if len(pixels) != 10*20 {
		t.Errorf("pixels len = %d, want 200", len(pixels))
	}
}

func TestToGrayscale_BlackPixelIsZero(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	pixels, _, _ := toGrayscale(img)
	if pixels[0] != 0 {
		t.Errorf("black pixel: lum = %d, want 0", pixels[0])
	}
}

func TestToGrayscale_WhitePixelIsMax(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	pixels, _, _ := toGrayscale(img)
	if pixels[0] != 255 {
		t.Errorf("white pixel: lum = %d, want 255", pixels[0])
	}
}

func TestToGrayscale_RedChannelContributes(t *testing.T) {
	// Pure red has BT.601 lum = 0.299 * 255 ≈ 76
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	pixels, _, _ := toGrayscale(img)
	want := uint8(299 * 255 / 1000) // 76
	if pixels[0] != want {
		t.Errorf("pure red lum = %d, want %d", pixels[0], want)
	}
}

// ── ApplyOrientation ─────────────────────────────────────────────────

func TestApplyOrientation_NoEXIF_ReturnsOriginal(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	raw := []byte("not a jpeg with exif")
	got := ApplyOrientation(img, raw)
	if got != img {
		t.Error("expected original image returned when EXIF cannot be decoded")
	}
}

// ── rotate180 ────────────────────────────────────────────────────────

func TestRotate180_CornerPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	topLeft := color.RGBA{R: 255, A: 255}
	topRight := color.RGBA{G: 255, A: 255}
	bottomLeft := color.RGBA{B: 255, A: 255}
	bottomRight := color.RGBA{R: 255, G: 255, A: 255}
	img.SetRGBA(0, 0, topLeft)
	img.SetRGBA(1, 0, topRight)
	img.SetRGBA(0, 1, bottomLeft)
	img.SetRGBA(1, 1, bottomRight)

	rot := rotate180(img)

	// After 180°: each corner maps to the diagonally opposite corner
	if rot.At(1, 1) != topLeft {
		t.Errorf("rotate180 (1,1) = %v, want topLeft %v", rot.At(1, 1), topLeft)
	}
	if rot.At(0, 0) != bottomRight {
		t.Errorf("rotate180 (0,0) = %v, want bottomRight %v", rot.At(0, 0), bottomRight)
	}
	if rot.At(1, 0) != bottomLeft {
		t.Errorf("rotate180 (1,0) = %v, want bottomLeft %v", rot.At(1, 0), bottomLeft)
	}
	if rot.At(0, 1) != topRight {
		t.Errorf("rotate180 (0,1) = %v, want topRight %v", rot.At(0, 1), topRight)
	}
}

// ── rotate90CW ───────────────────────────────────────────────────────

func TestRotate90CW_Dimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 6)) // w=4, h=6
	rot := rotate90CW(img)
	b := rot.Bounds()
	// 90° CW swaps width and height
	if b.Dx() != 6 || b.Dy() != 4 {
		t.Errorf("rotate90CW dims = %dx%d, want 6x4", b.Dx(), b.Dy())
	}
}

func TestRotate90CW_CornerPixels(t *testing.T) {
	// src(x,y) → dst(h-1-y, x)  for h=2
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	topLeft := color.RGBA{R: 255, A: 255}
	topRight := color.RGBA{G: 255, A: 255}
	bottomLeft := color.RGBA{B: 255, A: 255}
	bottomRight := color.RGBA{R: 255, G: 255, A: 255}
	img.SetRGBA(0, 0, topLeft)
	img.SetRGBA(1, 0, topRight)
	img.SetRGBA(0, 1, bottomLeft)
	img.SetRGBA(1, 1, bottomRight)

	rot := rotate90CW(img)
	// topLeft  (0,0) → dst(1, 0)
	// topRight (1,0) → dst(1, 1)
	// bottomLeft (0,1) → dst(0, 0)
	// bottomRight (1,1) → dst(0, 1)
	if rot.At(1, 0) != topLeft {
		t.Errorf("rotate90CW (1,0) = %v, want topLeft %v", rot.At(1, 0), topLeft)
	}
	if rot.At(0, 0) != bottomLeft {
		t.Errorf("rotate90CW (0,0) = %v, want bottomLeft %v", rot.At(0, 0), bottomLeft)
	}
	if rot.At(1, 1) != topRight {
		t.Errorf("rotate90CW (1,1) = %v, want topRight %v", rot.At(1, 1), topRight)
	}
	if rot.At(0, 1) != bottomRight {
		t.Errorf("rotate90CW (0,1) = %v, want bottomRight %v", rot.At(0, 1), bottomRight)
	}
}

// ── rotate90CCW ──────────────────────────────────────────────────────

func TestRotate90CCW_Dimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	rot := rotate90CCW(img)
	b := rot.Bounds()
	if b.Dx() != 6 || b.Dy() != 4 {
		t.Errorf("rotate90CCW dims = %dx%d, want 6x4", b.Dx(), b.Dy())
	}
}

func TestRotate90CCW_CornerPixels(t *testing.T) {
	// src(x,y) → dst(y, w-1-x)  for w=2
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	topLeft := color.RGBA{R: 255, A: 255}
	topRight := color.RGBA{G: 255, A: 255}
	bottomLeft := color.RGBA{B: 255, A: 255}
	bottomRight := color.RGBA{R: 255, G: 255, A: 255}
	img.SetRGBA(0, 0, topLeft)
	img.SetRGBA(1, 0, topRight)
	img.SetRGBA(0, 1, bottomLeft)
	img.SetRGBA(1, 1, bottomRight)

	rot := rotate90CCW(img)
	// topLeft  (0,0) → dst(0, 1)
	// topRight (1,0) → dst(0, 0)
	// bottomLeft (0,1) → dst(1, 1)
	// bottomRight (1,1) → dst(1, 0)
	if rot.At(0, 1) != topLeft {
		t.Errorf("rotate90CCW (0,1) = %v, want topLeft %v", rot.At(0, 1), topLeft)
	}
	if rot.At(0, 0) != topRight {
		t.Errorf("rotate90CCW (0,0) = %v, want topRight %v", rot.At(0, 0), topRight)
	}
	if rot.At(1, 1) != bottomLeft {
		t.Errorf("rotate90CCW (1,1) = %v, want bottomLeft %v", rot.At(1, 1), bottomLeft)
	}
	if rot.At(1, 0) != bottomRight {
		t.Errorf("rotate90CCW (1,0) = %v, want bottomRight %v", rot.At(1, 0), bottomRight)
	}
}

// ── truncate ──────────────────────────────────────────────────────────

func TestTruncate_ShortString(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	s := "hello"
	if got := truncate(s, len(s)); got != s {
		t.Errorf("got %q, want %q", got, s)
	}
}

func TestTruncate_LongStringEndsWithEllipsis(t *testing.T) {
	got := truncate("hello world", 8)
	if []rune(got)[len([]rune(got))-1] != '…' {
		t.Errorf("truncated string should end with '…', got %q", got)
	}
}

// ── NewDetector — error paths ─────────────────────────────────────────

func TestNewDetector_MissingFile_ReturnsError(t *testing.T) {
	_, err := NewDetector("/nonexistent/path/to/cascade", DetectParams{})
	if err == nil {
		t.Error("expected error for missing cascade file, got nil")
	}
}

func TestNewDetector_InvalidCascadeData_ReturnsError(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bad_cascade")
	if err := os.WriteFile(tmp, []byte("this is not a valid cascade binary"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, err := NewDetector(tmp, DetectParams{})
	if err == nil {
		t.Error("expected error for invalid cascade data, got nil")
	}
}

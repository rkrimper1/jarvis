package faceanalysis

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
)

// HUD cyan palette
var (
	hudCyan      = color.NRGBA{R: 0x00, G: 0xd4, B: 0xff, A: 0xff}
	hudCyanGlow  = color.NRGBA{R: 0x00, G: 0xd4, B: 0xff, A: 0x88}
	hudCyanFill  = color.NRGBA{R: 0x00, G: 0xd4, B: 0xff, A: 0x40}
	hudCyanDim   = color.NRGBA{R: 0x00, G: 0xd4, B: 0xff, A: 0xcc}
)

// Annotate draws HUD-style targeting triangles over each detected face,
// labels each with its sentiment and commentary, saves the result as a PNG
// in outputDir, and returns the filename (not the full path).
func Annotate(img image.Image, dets []Detection, results []FaceResult, outputDir string) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("annotate: mkdir: %w", err)
	}

	dc := gg.NewContextForImage(img)
	w := float64(dc.Width())
	h := float64(dc.Height())

	// Font — scale to ~3% of image width, clamped [22, 40]
	fontSize := math.Max(22, math.Min(40, w*0.03))
	face, err := loadFont(fontSize)
	if err == nil {
		dc.SetFontFace(face)
	}

	if len(dets) == 0 {
		// No faces — still write the image with a HUD "NO TARGETS ACQUIRED" banner
		drawNoBanner(dc, w, h, fontSize)
	}

	for i, det := range dets {
		res := FaceResult{Sentiment: "UNKNOWN", Commentary: ""}
		if i < len(results) {
			res = results[i]
		}
		drawHUDFace(dc, det, i+1, res, w, h, fontSize)
	}

	filename := fmt.Sprintf("faces_%s.png", time.Now().UTC().Format("20060102_150405_000"))
	outPath := filepath.Join(outputDir, filename)
	if err := dc.SavePNG(outPath); err != nil {
		return "", fmt.Errorf("annotate: save png: %w", err)
	}
	return filename, nil
}

// drawHUDFace renders the targeting triangle + callout label for one face.
func drawHUDFace(dc *gg.Context, det Detection, idx int, res FaceResult, imgW, imgH, fontSize float64) {
	pad := math.Max(16, float64(det.W)*0.22)

	// Triangle vertices: downward-pointing (base at top, apex at bottom-center)
	ax := float64(det.X) - pad
	ay := float64(det.Y) - pad
	bx := float64(det.X+det.W) + pad
	by := float64(det.Y) - pad
	cx := float64(det.X+det.W/2)
	cy := float64(det.Y+det.H) + pad

	// ── 1. Semi-transparent fill ─────────────────────────────────────
	dc.SetColor(hudCyanFill)
	dc.MoveTo(ax, ay)
	dc.LineTo(bx, by)
	dc.LineTo(cx, cy)
	dc.ClosePath()
	dc.Fill()

	// ── 2. Glow layer ────────────────────────────────────────────────
	dc.SetColor(hudCyanGlow)
	dc.SetLineWidth(6)
	dc.MoveTo(ax, ay)
	dc.LineTo(bx, by)
	dc.LineTo(cx, cy)
	dc.ClosePath()
	dc.Stroke()

	// ── 3. Solid outline ─────────────────────────────────────────────
	dc.SetColor(hudCyan)
	dc.SetLineWidth(2.5)
	dc.MoveTo(ax, ay)
	dc.LineTo(bx, by)
	dc.LineTo(cx, cy)
	dc.ClosePath()
	dc.Stroke()

	// ── 4. Corner tick marks ─────────────────────────────────────────
	tick := math.Max(6, fontSize*0.7)
	dc.SetColor(hudCyan)
	dc.SetLineWidth(2.5)

	// Top-left vertex A
	dc.DrawLine(ax-tick, ay, ax, ay)
	dc.Stroke()
	dc.DrawLine(ax, ay-tick, ax, ay)
	dc.Stroke()

	// Top-right vertex B
	dc.DrawLine(bx, by, bx+tick, by)
	dc.Stroke()
	dc.DrawLine(bx, by-tick, bx, by)
	dc.Stroke()

	// Apex C (bottom-center)
	dc.DrawLine(cx, cy, cx, cy+tick)
	dc.Stroke()
	dc.DrawLine(cx-tick*0.6, cy+tick, cx+tick*0.6, cy+tick)
	dc.Stroke()

	// ── 5. Face index label inside triangle ──────────────────────────
	dc.SetColor(hudCyan)
	dc.DrawStringAnchored(fmt.Sprintf("◉ %02d", idx),
		(ax+bx+cx)/3, (ay+by+cy)/3,
		0.5, 0.5)

	// ── 6. Callout line + text from top-right vertex ─────────────────
	calloutLen := math.Max(40, imgW*0.05)
	lineEndX := bx + calloutLen
	lineEndY := by - fontSize*0.5

	// Clamp callout to right edge
	if lineEndX > imgW-4 {
		lineEndX = bx - calloutLen
	}

	dc.SetColor(hudCyanDim)
	dc.SetLineWidth(1)
	dc.DrawLine(bx, by, lineEndX, lineEndY)
	dc.Stroke()
	// Small dot at end
	dc.DrawCircle(lineEndX, lineEndY, 2)
	dc.Fill()

	// Label text
	textX := lineEndX + 4
	if lineEndX < bx { // flipped left
		textX = lineEndX - 4
	}
	anchorX := 0.0
	if lineEndX < bx {
		anchorX = 1.0
	}

	dc.SetColor(hudCyan)
	dc.DrawStringAnchored(
		fmt.Sprintf("FACE %02d  ·  %s", idx, strings.ToUpper(res.Sentiment)),
		textX, lineEndY-fontSize*0.6,
		anchorX, 0.5,
	)
	dc.SetColor(hudCyanDim)
	dc.DrawStringAnchored(
		truncate(res.Commentary, 48),
		textX, lineEndY+fontSize*0.6,
		anchorX, 0.5,
	)
}

func drawNoBanner(dc *gg.Context, w, h, fontSize float64) {
	dc.SetColor(hudCyanDim)
	dc.SetLineWidth(1)
	// Horizontal scan lines across middle third
	for y := h * 0.4; y < h*0.6; y += fontSize * 1.4 {
		dc.DrawLine(0, y, w, y)
		dc.Stroke()
	}
	dc.SetColor(hudCyan)
	dc.DrawStringAnchored("NO TARGETS ACQUIRED", w/2, h/2, 0.5, 0.5)
}

func loadFont(size float64) (font.Face, error) {
	tt, err := truetype.Parse(gomono.TTF)
	if err != nil {
		return nil, err
	}
	return truetype.NewFace(tt, &truetype.Options{
		Size:    size,
		DPI:     96,
		Hinting: font.HintingFull,
	}), nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

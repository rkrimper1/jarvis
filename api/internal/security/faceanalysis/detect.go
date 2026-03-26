// Package faceanalysis detects faces in images and annotates them with
// HUD-style overlays using the pigo face detector and fogleman/gg renderer.
package faceanalysis

import (
	"fmt"
	"image"
	"os"

	pigo "github.com/esimov/pigo/core"
)

// Detection holds the bounding box for one detected face.
type Detection struct {
	X, Y, W, H int
}

// Detect loads the pigo cascade from cascadePath and returns bounding boxes
// for all faces found in img. Returns an empty slice (not an error) when no
// faces are detected.
func Detect(img image.Image, cascadePath string) ([]Detection, error) {
	cascadeData, err := os.ReadFile(cascadePath)
	if err != nil {
		return nil, fmt.Errorf("faceanalysis: read cascade: %w", err)
	}

	classifier, err := new(pigo.Pigo).Unpack(cascadeData)
	if err != nil {
		return nil, fmt.Errorf("faceanalysis: unpack cascade: %w", err)
	}

	pixels, cols, rows := toGrayscale(img)

	cParams := pigo.CascadeParams{
		MinSize:     30,
		MaxSize:     min(cols, rows),
		ShiftFactor: 0.1,
		ScaleFactor: 1.1,
		ImageParams: pigo.ImageParams{
			Pixels: pixels,
			Rows:   rows,
			Cols:   cols,
			Dim:    cols,
		},
	}

	dets := classifier.RunCascade(cParams, 0.0)
	dets = classifier.ClusterDetections(dets, 0.15)

	out := make([]Detection, 0, len(dets))
	for _, d := range dets {
		if d.Q < 6.0 {
			continue
		}
		half := int(d.Scale / 2)
		x := d.Col - half
		y := d.Row - half
		w := int(d.Scale)
		h := int(d.Scale)
		// Clamp to image bounds
		if x < 0 { x = 0 }
		if y < 0 { y = 0 }
		if x+w > cols { w = cols - x }
		if y+h > rows { h = rows - y }
		if w > 0 && h > 0 {
			out = append(out, Detection{X: x, Y: y, W: w, H: h})
		}
	}
	return out, nil
}

// toGrayscale converts any image.Image to a flat uint8 grayscale pixel slice
// in row-major order, as required by pigo.
func toGrayscale(img image.Image) ([]uint8, int, int) {
	b := img.Bounds()
	cols, rows := b.Dx(), b.Dy()
	pixels := make([]uint8, cols*rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			r, g, bl, _ := img.At(x+b.Min.X, y+b.Min.Y).RGBA()
			// Luminance (BT.601) — RGBA values are 16-bit, shift to 8-bit
			lum := (299*uint32(r>>8) + 587*uint32(g>>8) + 114*uint32(bl>>8)) / 1000
			pixels[y*cols+x] = uint8(lum)
		}
	}
	return pixels, cols, rows
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

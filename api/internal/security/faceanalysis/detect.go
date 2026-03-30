// Package faceanalysis detects faces in images and annotates them with
// HUD-style overlays using the pigo face detector and fogleman/gg renderer.
package faceanalysis

import (
	"context"
	"fmt"
	"image"
	"os"

	pigo "github.com/esimov/pigo/core"
	"go.opencensus.io/trace"
)

// Detection holds the bounding box for one detected face.
type Detection struct {
	X, Y, W, H int
}

// DetectParams controls the tunable detection parameters.
type DetectParams struct {
	MinSize          int     // minimum face size in pixels (default 65)
	QualityThreshold float32 // minimum pigo quality score (default 6.0)
	ClusterOverlap   float64 // clustering overlap factor (default 0.25)
}

// DefaultDetectParams returns sensible defaults.
func DefaultDetectParams() DetectParams {
	return DetectParams{
		MinSize:          65,
		QualityThreshold: 6.0,
		ClusterOverlap:   0.25,
	}
}

// Detector holds a pre-loaded pigo classifier and its tuning parameters.
// Construct once at startup via NewDetector and reuse across requests.
type Detector struct {
	classifier *pigo.Pigo
	params     DetectParams
}

// NewDetector loads and parses the pigo cascade file at cascadePath.
// Returns an error if the file cannot be read or parsed, surfacing
// misconfiguration at startup rather than on the first request.
func NewDetector(cascadePath string, params DetectParams) (_ *Detector, err error) {
	cascadeData, err := os.ReadFile(cascadePath)
	if err != nil {
		return nil, fmt.Errorf("faceanalysis: read cascade: %w", err)
	}

	// pigo.Unpack panics on malformed data rather than returning an error;
	// recover and convert to a normal error so the server does not crash.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("faceanalysis: unpack cascade: %v", r)
		}
	}()

	classifier, err := new(pigo.Pigo).Unpack(cascadeData)
	if err != nil {
		return nil, fmt.Errorf("faceanalysis: unpack cascade: %w", err)
	}
	return &Detector{classifier: classifier, params: params}, nil
}

// Detect returns bounding boxes for all faces found in img.
// Returns an empty slice (not an error) when no faces are detected.
func (d *Detector) Detect(ctx context.Context, img image.Image) ([]Detection, error) {
	_, span := trace.StartSpan(ctx, "jarvis/faceanalysis.Detect")
	defer span.End()

	pixels, cols, rows := toGrayscale(img)

	cParams := pigo.CascadeParams{
		MinSize:     d.params.MinSize,
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

	dets := d.classifier.RunCascade(cParams, 0.0)
	dets = d.classifier.ClusterDetections(dets, d.params.ClusterOverlap)

	out := make([]Detection, 0, len(dets))
	for _, det := range dets {
		if det.Q < d.params.QualityThreshold {
			continue
		}
		half := int(det.Scale / 2)
		x := det.Col - half
		y := det.Row - half
		w := int(det.Scale)
		h := int(det.Scale)
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


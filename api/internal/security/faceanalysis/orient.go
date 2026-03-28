package faceanalysis

import (
	"bytes"
	"image"
	"image/draw"

	"github.com/rwcarlsen/goexif/exif"
)

// ApplyOrientation reads the EXIF orientation tag from raw and returns a
// correctly-rotated copy of img. If no EXIF data is present, img is returned
// unchanged.
func ApplyOrientation(img image.Image, raw []byte) image.Image {
	x, err := exif.Decode(bytes.NewReader(raw))
	if err != nil {
		return img
	}
	tag, err := x.Get(exif.Orientation)
	if err != nil {
		return img
	}
	orient, err := tag.Int(0)
	if err != nil {
		return img
	}

	switch orient {
	case 3: // 180°
		return rotate180(img)
	case 6: // 90° CW (phone portrait stored as landscape)
		return rotate90CW(img)
	case 8: // 90° CCW
		return rotate90CCW(img)
	}
	return img
}

func rotate90CW(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.Y-1-y, x-b.Min.X, src.At(x, y))
		}
	}
	return dst
}

func rotate90CCW(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(y-b.Min.Y, b.Max.X-1-x, src.At(x, y))
		}
	}
	return dst
}

func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	w, h := b.Dx(), b.Dy()
	for y := 0; y < h/2; y++ {
		for x := 0; x < w; x++ {
			a := dst.At(b.Min.X+x, b.Min.Y+y)
			b2 := dst.At(b.Max.X-1-x, b.Max.Y-1-y)
			dst.Set(b.Min.X+x, b.Min.Y+y, b2)
			dst.Set(b.Max.X-1-x, b.Max.Y-1-y, a)
		}
	}
	return dst
}

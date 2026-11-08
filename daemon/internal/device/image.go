package device

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"

	xdraw "golang.org/x/image/draw"
)

const (
	pixelFormatRGBA8888 = 1
	pixelFormatRGBX8888 = 2
	maxScreenDim        = 16384
)

// parseRawScreencap decodes `screencap` output without -p: a little-endian
// header (width, height, format, and on Android 9+ a colorspace word)
// followed by width*height*4 bytes. The header size is inferred from length.
func parseRawScreencap(data []byte) (*image.RGBA, error) {
	if len(data) < 12 {
		return nil, errors.New("screencap: output too short")
	}
	w := binary.LittleEndian.Uint32(data[0:])
	h := binary.LittleEndian.Uint32(data[4:])
	format := binary.LittleEndian.Uint32(data[8:])
	if w == 0 || h == 0 || w > maxScreenDim || h > maxScreenDim {
		return nil, fmt.Errorf("screencap: bad dimensions %dx%d", w, h)
	}
	if format != pixelFormatRGBA8888 && format != pixelFormatRGBX8888 {
		return nil, fmt.Errorf("unsupported screencap pixel format %d", format)
	}
	pixels := int(w) * int(h) * 4
	hdr := len(data) - pixels
	if hdr != 12 && hdr != 16 {
		return nil, fmt.Errorf("screencap: %d bytes does not match %dx%d RGBA", len(data), w, h)
	}
	img := &image.RGBA{
		Pix:    data[hdr:],
		Stride: int(w) * 4,
		Rect:   image.Rect(0, 0, int(w), int(h)),
	}
	if format == pixelFormatRGBX8888 {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xFF
		}
	}
	return img, nil
}

// encodeScreenshot scales img by scale (<=1) and encodes it as JPEG.
func encodeScreenshot(img *image.RGBA, scale float64, format string, quality int) ([]byte, string, error) {
	switch format {
	case "", "jpeg":
	case "webp":
		// shortcut: no stdlib WebP encoder; the Phase 1 helper uses Bitmap.compress(WEBP).
		return nil, "", errors.New("webp is not supported by the exec backend yet; use jpeg")
	default:
		return nil, "", fmt.Errorf("unknown format %q (want jpeg or webp)", format)
	}
	var src image.Image = img
	if scale < 1 {
		b := img.Bounds()
		dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(b.Dx())*scale+0.5)), max(1, int(float64(b.Dy())*scale+0.5))))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		src = dst
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: quality}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// looksBlack samples a grid and reports whether every sample is near black,
// which is what FLAG_SECURE windows produce.
func looksBlack(img *image.RGBA) bool {
	b := img.Bounds()
	const grid = 16
	for gy := range grid {
		for gx := range grid {
			x := b.Min.X + (b.Dx()-1)*gx/(grid-1)
			y := b.Min.Y + (b.Dy()-1)*gy/(grid-1)
			i := img.PixOffset(x, y)
			if img.Pix[i] > 8 || img.Pix[i+1] > 8 || img.Pix[i+2] > 8 {
				return false
			}
		}
	}
	return true
}

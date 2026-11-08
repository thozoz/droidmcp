package device

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"strings"
	"testing"
)

// rawCap builds screencap raw output with a hdrLen-byte header (12 or 16)
// and every pixel set to px.
func rawCap(w, h, format uint32, hdrLen int, px [4]byte) []byte {
	b := make([]byte, hdrLen, hdrLen+int(w*h*4))
	binary.LittleEndian.PutUint32(b[0:], w)
	binary.LittleEndian.PutUint32(b[4:], h)
	binary.LittleEndian.PutUint32(b[8:], format)
	for range w * h {
		b = append(b, px[:]...)
	}
	return b
}

func TestParseRawScreencap(t *testing.T) {
	for _, hdr := range []int{12, 16} {
		img, err := parseRawScreencap(rawCap(4, 3, pixelFormatRGBA8888, hdr, [4]byte{10, 20, 30, 40}))
		if err != nil {
			t.Fatalf("hdr %d: %v", hdr, err)
		}
		if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 3 {
			t.Errorf("hdr %d: bounds %v", hdr, img.Bounds())
		}
		if got := img.RGBAAt(3, 2); got.R != 10 || got.A != 40 {
			t.Errorf("hdr %d: pixel %v", hdr, got)
		}
	}
}

func TestParseRawScreencapRGBXForcesAlpha(t *testing.T) {
	img, err := parseRawScreencap(rawCap(2, 2, pixelFormatRGBX8888, 16, [4]byte{1, 2, 3, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if a := img.RGBAAt(1, 1).A; a != 0xFF {
		t.Errorf("alpha = %d, want 255", a)
	}
}

func TestParseRawScreencapErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"too short", []byte{1, 2, 3}, "too short"},
		{"unsupported format", rawCap(2, 2, 5, 16, [4]byte{}), "unsupported screencap pixel format 5"},
		{"RGB_565", rawCap(2, 2, 4, 16, [4]byte{}), "unsupported screencap pixel format 4"},
		{"bad length", rawCap(2, 2, pixelFormatRGBA8888, 16, [4]byte{})[:20], "does not match"},
		{"zero size", rawCap(0, 2, pixelFormatRGBA8888, 12, [4]byte{}), "bad dimensions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseRawScreencap(tt.data)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLooksBlack(t *testing.T) {
	black, _ := parseRawScreencap(rawCap(40, 40, pixelFormatRGBA8888, 16, [4]byte{0, 0, 0, 255}))
	if !looksBlack(black) {
		t.Error("black image not detected")
	}
	gray, _ := parseRawScreencap(rawCap(40, 40, pixelFormatRGBA8888, 16, [4]byte{90, 90, 90, 255}))
	if looksBlack(gray) {
		t.Error("gray image flagged black")
	}
}

func TestEncodeScreenshot(t *testing.T) {
	img, _ := parseRawScreencap(rawCap(100, 200, pixelFormatRGBA8888, 16, [4]byte{200, 100, 50, 255}))
	data, mime, err := encodeScreenshot(img, 0.5, "jpeg", 70)
	if err != nil || mime != "image/jpeg" {
		t.Fatalf("err=%v mime=%s", err, mime)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 50 || cfg.Height != 100 {
		t.Fatalf("decoded %+v err=%v", cfg, err)
	}
	if _, _, err := encodeScreenshot(img, 1, "webp", 70); err == nil {
		t.Error("webp should error in exec backend")
	}
}

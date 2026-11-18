package appearance

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func encodeTestGrayPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) % 256)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode gray png: %v", err)
	}
	return buf.Bytes()
}

// TestNormalizeRejectsPixelBomb 验证 DecodeConfig 尺寸/像素上限先于完整解码。
func TestNormalizeRejectsPixelBomb(t *testing.T) {
	// 4000x3001 = 12,004,000 像素 > 12MP，但单边仍在 4096 以内。
	content := encodeTestGrayPNG(t, 4000, 3001)
	if _, err := normalizeBackgroundImage(content); err == nil {
		t.Fatal("expected pixel bomb rejection")
	} else {
		requireCode(t, err, "background_dimensions_exceeded")
	}
}

// TestNormalizeResizesMasterPreservingAspect 验证最大边 2560 与等比缩放。
func TestNormalizeResizesMasterPreservingAspect(t *testing.T) {
	normalized, err := normalizeBackgroundImage(encodeTestJPEG(t, 1500, 3000))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized.Mime != "image/jpeg" || normalized.Width != 1280 || normalized.Height != 2560 {
		t.Fatalf("normalized = mime:%s %dx%d", normalized.Mime, normalized.Width, normalized.Height)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(normalized.Master))
	if err != nil {
		t.Fatalf("decode master: %v", err)
	}
	if decoded.Bounds().Dx() != 1280 || decoded.Bounds().Dy() != 2560 {
		t.Fatalf("master = %dx%d", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
	thumb, err := jpeg.Decode(bytes.NewReader(normalized.Thumb))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	if thumb.Bounds().Dx() != thumbWidth || thumb.Bounds().Dy() != thumbHeight {
		t.Fatalf("thumb = %dx%d", thumb.Bounds().Dx(), thumb.Bounds().Dy())
	}
}

// TestNormalizeSmallImageUpscalesThumbnail 验证小图也生成固定 160x90 cover 缩略图。
func TestNormalizeSmallImageUpscalesThumbnail(t *testing.T) {
	normalized, err := normalizeBackgroundImage(encodeTestPNG(t, 40, 20, false))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized.Width != 40 || normalized.Height != 20 {
		t.Fatalf("small image must not be upscaled in master: %dx%d", normalized.Width, normalized.Height)
	}
	thumb, err := jpeg.Decode(bytes.NewReader(normalized.Thumb))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	if thumb.Bounds().Dx() != thumbWidth || thumb.Bounds().Dy() != thumbHeight {
		t.Fatalf("thumb = %dx%d", thumb.Bounds().Dx(), thumb.Bounds().Dy())
	}
}

// TestNormalizeStripsMetadata 验证 JPEG 重编码剥离 APP1/EXIF 段。
func TestNormalizeStripsMetadata(t *testing.T) {
	base := encodeTestJPEG(t, 200, 100)
	// 在 SOI 之后插入一个 APP1 段（长度 0x0010，含 "Exif"），模拟带 EXIF 的 JPEG。
	app1 := []byte{0xFF, 0xE1, 0x00, 0x10, 'E', 'x', 'i', 'f', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	withExif := append(append([]byte{0xFF, 0xD8}, app1...), base[2:]...)
	normalized, err := normalizeBackgroundImage(withExif)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if bytes.Contains(normalized.Master, []byte("Exif")) {
		t.Fatal("normalized master must not carry EXIF metadata")
	}
	if _, err := jpeg.Decode(bytes.NewReader(normalized.Master)); err != nil {
		t.Fatalf("decode normalized master: %v", err)
	}
}

// TestNormalizeDetectsFormatByMagic 验证按魔数而非文件名判断格式。
func TestNormalizeDetectsFormatByMagic(t *testing.T) {
	normalized, err := normalizeBackgroundImage(encodeTestPNG(t, 30, 30, false))
	if err != nil {
		t.Fatalf("normalize png: %v", err)
	}
	if normalized.Mime != "image/png" {
		t.Fatalf("mime = %q", normalized.Mime)
	}
}

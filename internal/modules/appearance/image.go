package appearance

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"

	panelerr "panel/internal/platform/errors"
)

// 背景图片管线常量。全部使用 Go 标准库，不引入新的图片/缩放依赖。
const (
	// MaxBackgroundUploadBytes 是单次背景上传允许的原始文件上限（8 MiB）。
	MaxBackgroundUploadBytes = 8 << 20
	// maxBackgroundDimension 是允许的最大单边像素数。
	maxBackgroundDimension = 4096
	// maxBackgroundPixels 是允许的最大总像素数（12 MP，防解压炸弹）。
	maxBackgroundPixels = 12_000_000
	// maxBackgroundEdge 是归一化入库后允许的最大边长。
	maxBackgroundEdge = 2560
	// thumbWidth/thumbHeight 是 Mica 底图缩略图尺寸（cover）。
	thumbWidth  = 160
	thumbHeight = 90
	// masterJPEGQuality 是 JPEG 归一化重编码质量（顺带剥离 EXIF 等元数据）。
	masterJPEGQuality = 87
	// thumbJPEGQuality 是缩略图 JPEG 质量。
	thumbJPEGQuality = 70
	// maxThumbBytes 是缩略图允许的最大字节数。
	maxThumbBytes = 64 << 10
)

// backgroundTooLargeError 返回上传超限的稳定错误（413）。
func backgroundTooLargeError() *panelerr.Error {
	return panelerr.New(http.StatusRequestEntityTooLarge, "background_too_large", "Background image must be 8 MiB or smaller")
}

// backgroundUnsupportedTypeError 返回非 JPEG/PNG 的稳定错误（415）。
func backgroundUnsupportedTypeError() *panelerr.Error {
	return panelerr.New(http.StatusUnsupportedMediaType, "background_unsupported_type", "Background image must be a JPEG or PNG file")
}

// backgroundInvalidImageError 返回无法解码/损坏图片的稳定错误。
func backgroundInvalidImageError() *panelerr.Error {
	return panelerr.Validation("background_invalid_image", "Background image is invalid or corrupted")
}

// backgroundDimensionsExceededError 返回尺寸/像素超限的稳定错误。
func backgroundDimensionsExceededError() *panelerr.Error {
	return panelerr.Validation("background_dimensions_exceeded", "Background image cannot exceed 4096x4096 or 12 megapixels")
}

// normalizedBackground 是图片管线的输出：归一化后的 master 与 160x90 缩略图。
type normalizedBackground struct {
	Master []byte
	Mime   string
	Width  int
	Height int
	Thumb  []byte
}

// normalizeBackgroundImage 校验并归一化上传图片：
//   - 只接受 JPEG/PNG（按魔数而非文件名/Content-Type 判断；GIF/WebP 明确拒绝）；
//   - 先用 image.DecodeConfig 读尺寸，拒绝 >4096x4096 或 >12MP 的图片，避免解压炸弹；
//   - JPEG 以 quality 87 重编码（剥离 EXIF），PNG 重编码保留透明通道；
//   - 最大边超过 2560 时用盒子采样等比缩小；
//   - 生成 160x90（cover）JPEG q70 缩略图。
func normalizeBackgroundImage(content []byte) (normalizedBackground, error) {
	if len(content) == 0 {
		return normalizedBackground{}, backgroundRequiredError()
	}
	if len(content) > MaxBackgroundUploadBytes {
		return normalizedBackground{}, backgroundTooLargeError()
	}
	mimeType := http.DetectContentType(content)
	if mimeType != "image/jpeg" && mimeType != "image/png" {
		return normalizedBackground{}, backgroundUnsupportedTypeError()
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return normalizedBackground{}, backgroundInvalidImageError()
	}
	if config.Width <= 0 || config.Height <= 0 {
		return normalizedBackground{}, backgroundInvalidImageError()
	}
	if config.Width > maxBackgroundDimension || config.Height > maxBackgroundDimension ||
		int64(config.Width)*int64(config.Height) > maxBackgroundPixels {
		return normalizedBackground{}, backgroundDimensionsExceededError()
	}
	decoded, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return normalizedBackground{}, backgroundInvalidImageError()
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return normalizedBackground{}, backgroundInvalidImageError()
	}

	masterImage := image.Image(decoded)
	if bounds.Dx() > maxBackgroundEdge || bounds.Dy() > maxBackgroundEdge {
		masterImage = fitWithinEdge(decoded, maxBackgroundEdge)
	}
	var master bytes.Buffer
	if mimeType == "image/jpeg" {
		if err := jpeg.Encode(&master, masterImage, &jpeg.Options{Quality: masterJPEGQuality}); err != nil {
			return normalizedBackground{}, backgroundInvalidImageError()
		}
	} else {
		if err := png.Encode(&master, masterImage); err != nil {
			return normalizedBackground{}, backgroundInvalidImageError()
		}
	}
	if master.Len() > MaxBackgroundUploadBytes {
		return normalizedBackground{}, backgroundTooLargeError()
	}

	thumbImage := coverThumbnail(decoded, thumbWidth, thumbHeight)
	var thumb bytes.Buffer
	if err := jpeg.Encode(&thumb, thumbImage, &jpeg.Options{Quality: thumbJPEGQuality}); err != nil {
		return normalizedBackground{}, backgroundInvalidImageError()
	}
	if thumb.Len() > maxThumbBytes {
		return normalizedBackground{}, backgroundInvalidImageError()
	}

	return normalizedBackground{
		Master: master.Bytes(),
		Mime:   mimeType,
		Width:  masterImage.Bounds().Dx(),
		Height: masterImage.Bounds().Dy(),
		Thumb:  thumb.Bytes(),
	}, nil
}

// fitWithinEdge 等比缩放到最长边不超过 maxEdge（只缩小，不放大）。
func fitWithinEdge(src image.Image, maxEdge int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	longest := width
	if height > longest {
		longest = height
	}
	if longest <= maxEdge || longest == 0 {
		return src
	}
	scale := float64(maxEdge) / float64(longest)
	targetWidth := int(math.Round(float64(width) * scale))
	targetHeight := int(math.Round(float64(height) * scale))
	if targetWidth < 1 {
		targetWidth = 1
	}
	if targetHeight < 1 {
		targetHeight = 1
	}
	return resizeImage(src, targetWidth, targetHeight)
}

// coverThumbnail 先等比缩放到覆盖目标尺寸，再居中裁剪（cover 语义）。
func coverThumbnail(src image.Image, width, height int) image.Image {
	bounds := src.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return image.NewRGBA(image.Rect(0, 0, width, height))
	}
	scale := math.Max(float64(width)/float64(sourceWidth), float64(height)/float64(sourceHeight))
	scaledWidth := int(math.Round(float64(sourceWidth) * scale))
	scaledHeight := int(math.Round(float64(sourceHeight) * scale))
	if scaledWidth < width {
		scaledWidth = width
	}
	if scaledHeight < height {
		scaledHeight = height
	}
	scaled := resizeImage(src, scaledWidth, scaledHeight)
	offsetX := (scaledWidth - width) / 2
	offsetY := (scaledHeight - height) / 2
	cropped := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcStart := (y+offsetY)*scaled.Stride + offsetX*4
		dstStart := y * cropped.Stride
		copy(cropped.Pix[dstStart:dstStart+width*4], scaled.Pix[srcStart:srcStart+width*4])
	}
	return cropped
}

// resizeImage 使用盒子采样（area average）缩放图片。缩放与放大都可用：
// 放大时每个目标像素退化为最近的源像素面积权重。采样在预乘 alpha 空间进行，
// 透明 PNG 缩小后不会出现黑边。
func resizeImage(src image.Image, width, height int) *image.RGBA {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	bounds := src.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return dst
	}
	for y := 0; y < height; y++ {
		sourceTop := float64(y) * float64(sourceHeight) / float64(height)
		sourceBottom := float64(y+1) * float64(sourceHeight) / float64(height)
		firstY := int(math.Floor(sourceTop))
		lastY := int(math.Ceil(sourceBottom))
		if lastY <= firstY {
			lastY = firstY + 1
		}
		if firstY < 0 {
			firstY = 0
		}
		if lastY > sourceHeight {
			lastY = sourceHeight
		}
		for x := 0; x < width; x++ {
			sourceLeft := float64(x) * float64(sourceWidth) / float64(width)
			sourceRight := float64(x+1) * float64(sourceWidth) / float64(width)
			firstX := int(math.Floor(sourceLeft))
			lastX := int(math.Ceil(sourceRight))
			if lastX <= firstX {
				lastX = firstX + 1
			}
			if firstX < 0 {
				firstX = 0
			}
			if lastX > sourceWidth {
				lastX = sourceWidth
			}
			var redSum, greenSum, blueSum, alphaSum, weightSum float64
			for sourceY := firstY; sourceY < lastY; sourceY++ {
				weightY := overlap(sourceTop, sourceBottom, float64(sourceY), float64(sourceY+1))
				if weightY <= 0 {
					continue
				}
				for sourceX := firstX; sourceX < lastX; sourceX++ {
					weightX := overlap(sourceLeft, sourceRight, float64(sourceX), float64(sourceX+1))
					if weightX <= 0 {
						continue
					}
					weight := weightX * weightY
					red, green, blue, alpha := src.At(bounds.Min.X+sourceX, bounds.Min.Y+sourceY).RGBA()
					redSum += float64(red) * weight
					greenSum += float64(green) * weight
					blueSum += float64(blue) * weight
					alphaSum += float64(alpha) * weight
					weightSum += weight
				}
			}
			offset := y*dst.Stride + x*4
			if weightSum <= 0 {
				continue
			}
			dst.Pix[offset+0] = to8Bit(redSum / weightSum)
			dst.Pix[offset+1] = to8Bit(greenSum / weightSum)
			dst.Pix[offset+2] = to8Bit(blueSum / weightSum)
			dst.Pix[offset+3] = to8Bit(alphaSum / weightSum)
		}
	}
	return dst
}

// overlap 返回两个区间的重叠长度。
func overlap(start, end, blockStart, blockEnd float64) float64 {
	low := math.Max(start, blockStart)
	high := math.Min(end, blockEnd)
	if high <= low {
		return 0
	}
	return high - low
}

// to8Bit 把 16 位预乘颜色分量转换为 8 位预乘分量。
func to8Bit(value16 float64) uint8 {
	value := math.Round(value16 / 257)
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

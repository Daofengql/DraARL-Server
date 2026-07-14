package storage

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
)

func newUUID() string {
	return uuid.New().String()
}

// ProcessAvatar 处理头像：限制 2000、中心裁切正方形、JPEG 编码。
func ProcessAvatar(fileHeader *multipart.FileHeader) ([]byte, string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, "", fmt.Errorf("解码图片失败: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	const maxSize = 2000
	if width > maxSize || height > maxSize {
		scale := float64(maxSize) / float64(max(width, height))
		img = imaging.Resize(img, int(float64(width)*scale), int(float64(height)*scale), imaging.Lanczos)
		bounds = img.Bounds()
		width = bounds.Dx()
		height = bounds.Dy()
	}

	var cropped image.Image
	if width != height {
		size := width
		if height < size {
			size = height
		}
		x := (width - size) / 2
		y := (height - size) / 2
		cropped = imaging.Crop(img, image.Rect(x, y, x+size, y+size))
	} else {
		cropped = img
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, cropped, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", fmt.Errorf("编码图片失败: %w", err)
	}
	ext := filepath.Ext(fileHeader.Filename)
	if ext == "" {
		ext = ".jpg"
	}
	return buf.Bytes(), ext, nil
}

// GenerateThumbnail 从存储读取原图并生成缩略图数据。
func GenerateThumbnail(originalObject string, width, height int, ext string) (string, []byte, error) {
	thumbObjectName := "thumb/" + originalObject
	reader, err := Open(context.Background(), originalObject)
	if err != nil {
		return "", nil, fmt.Errorf("下载原图片失败: %w", err)
	}
	defer reader.Close()

	img, _, err := image.Decode(reader)
	if err != nil {
		return "", nil, fmt.Errorf("解码图片失败: %w", err)
	}
	thumbnail := imaging.Resize(img, width, height, imaging.Lanczos)

	var buf bytes.Buffer
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(&buf, thumbnail, &jpeg.Options{Quality: 85})
	case ".png":
		err = png.Encode(&buf, thumbnail)
	default:
		err = jpeg.Encode(&buf, thumbnail, &jpeg.Options{Quality: 85})
	}
	if err != nil {
		return "", nil, fmt.Errorf("编码缩略图失败: %w", err)
	}
	return thumbObjectName, buf.Bytes(), nil
}

// ProcessLogo 处理 Logo：限制 500x500，输出 PNG。
func ProcessLogo(fileHeader *multipart.FileHeader) ([]byte, string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	buf := make([]byte, fileHeader.Size)
	_, err = io.ReadFull(file, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, "", fmt.Errorf("读取文件失败: %w", err)
	}

	var img image.Image
	var format string
	var decodeErr error

	reader := bytes.NewReader(buf)
	img, format, decodeErr = image.Decode(reader)
	if decodeErr != nil {
		reader = bytes.NewReader(buf)
		img, decodeErr = imaging.Decode(reader, imaging.AutoOrientation(true))
		if decodeErr != nil {
			reader = bytes.NewReader(buf)
			if img, decodeErr = png.Decode(reader); decodeErr == nil {
				format = "png"
			} else {
				reader = bytes.NewReader(buf)
				if img, decodeErr = jpeg.Decode(reader); decodeErr == nil {
					format = "jpeg"
				} else {
					return nil, "", fmt.Errorf("解码图片失败: %w", decodeErr)
				}
			}
		}
	}
	_ = format

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	const maxLogoWidth = 500
	const maxLogoHeight = 500
	if width > maxLogoWidth || height > maxLogoHeight {
		scaleX := float64(maxLogoWidth) / float64(width)
		scaleY := float64(maxLogoHeight) / float64(height)
		scale := scaleX
		if scaleY < scaleX {
			scale = scaleY
		}
		img = imaging.Resize(img, int(float64(width)*scale), int(float64(height)*scale), imaging.Lanczos)
	}

	var outputBuf bytes.Buffer
	if err := png.Encode(&outputBuf, img); err != nil {
		return nil, "", fmt.Errorf("编码图片失败: %w", err)
	}
	return outputBuf.Bytes(), ".png", nil
}

package icon

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"regexp"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const svgRenderSize = 512

var ErrNoPicture = errors.New("картинки нет")

func FromDataURI(uri string) (image.Image, error) {
	if strings.TrimSpace(uri) == "" {
		return nil, ErrNoPicture
	}
	if !strings.HasPrefix(uri, "data:") {
		return nil, fmt.Errorf("оформление хранит картинку не как data URI")
	}
	head, payload, ok := strings.Cut(strings.TrimPrefix(uri, "data:"), ",")
	if !ok {
		return nil, fmt.Errorf("картинка оформления обрезана")
	}
	if !strings.Contains(head, "base64") {
		return nil, fmt.Errorf("картинка оформления записана не в base64")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("картинка оформления не читается: %w", err)
	}
	return Decode(data, strings.Split(head, ";")[0])
}

func Decode(data []byte, mime string) (image.Image, error) {
	if len(data) == 0 {
		return nil, ErrNoPicture
	}
	if strings.Contains(mime, "svg") || bytes.Contains(peek(data), []byte("<svg")) {
		return renderSVG(data)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("картинку не удалось прочитать: %w", err)
	}
	return decoded, nil
}

func peek(data []byte) []byte {
	if len(data) > 512 {
		return data[:512]
	}
	return data
}

func renderSVG(data []byte) (image.Image, error) {
	parsed, err := oksvg.ReadIconStream(bytes.NewReader(evenCorners(data)))
	if err != nil {
		return nil, fmt.Errorf("SVG не разобрался: %w", err)
	}
	width, height := parsed.ViewBox.W, parsed.ViewBox.H
	if width <= 0 || height <= 0 {
		width, height = svgRenderSize, svgRenderSize
	}
	scale := float64(svgRenderSize) / max(width, height)
	target := image.NewRGBA(image.Rect(0, 0, int(width*scale), int(height*scale)))
	if target.Bounds().Empty() {
		return nil, fmt.Errorf("SVG пустой")
	}
	parsed.SetTarget(0, 0, float64(target.Bounds().Dx()), float64(target.Bounds().Dy()))
	for i := range parsed.SVGPaths {
		parsed.SVGPaths[i].LineWidth *= scale
		for dash := range parsed.SVGPaths[i].Dash {
			parsed.SVGPaths[i].Dash[dash] *= scale
		}
	}
	scanner := rasterx.NewScannerGV(target.Bounds().Dx(), target.Bounds().Dy(), target, target.Bounds())
	parsed.Draw(rasterx.NewDasher(target.Bounds().Dx(), target.Bounds().Dy(), scanner), 1)
	return target, nil
}

var lonelyRadius = regexp.MustCompile(`<rect\b[^>]*\brx="([^"]+)"[^>]*>`)

func evenCorners(data []byte) []byte {
	return lonelyRadius.ReplaceAllFunc(data, func(tag []byte) []byte {
		if bytes.Contains(tag, []byte(` ry="`)) {
			return tag
		}
		radius := lonelyRadius.FindSubmatch(tag)[1]
		return bytes.Replace(tag, []byte(` rx="`+string(radius)+`"`), []byte(` rx="`+string(radius)+`" ry="`+string(radius)+`"`), 1)
	})
}

func Square(source image.Image, size int) *image.RGBA {
	target := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(target, target.Bounds(), image.NewUniform(color.Transparent), image.Point{}, draw.Src)

	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width == 0 || height == 0 {
		return target
	}
	scale := min(float64(size)/float64(width), float64(size)/float64(height))
	scaled := image.Rect(0, 0, int(float64(width)*scale), int(float64(height)*scale))
	if scaled.Dx() == 0 || scaled.Dy() == 0 {
		return target
	}
	offset := image.Pt((size-scaled.Dx())/2, (size-scaled.Dy())/2)
	draw.CatmullRom.Scale(target, scaled.Add(offset), source, bounds, draw.Over, nil)
	return target
}

func PNG(source image.Image) ([]byte, error) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

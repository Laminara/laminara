package icon

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

const roundedMark = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <rect width="32" height="32" rx="8" fill="#f4d089"/>
  <path d="M6 16 L26 16" fill="none" stroke="#241705" stroke-width="4"/>
</svg>`

func transparent(picture image.Image, x, y int) bool {
	_, _, _, alpha := picture.At(x, y).RGBA()
	return alpha == 0
}

func TestSVGCornersStayRounded(t *testing.T) {
	picture, err := Decode([]byte(roundedMark), "image/svg+xml")
	if err != nil {
		t.Fatal(err)
	}
	if !transparent(picture, 1, 1) {
		t.Fatal("угол скруглённого прямоугольника должен остаться прозрачным: rx без ry читается как оба радиуса")
	}
	middle := picture.Bounds().Dx() / 2
	if transparent(picture, middle, picture.Bounds().Dy()/2) {
		t.Fatal("середина должна быть закрашена")
	}
}

func TestSVGStrokeScalesWithTheDrawing(t *testing.T) {
	picture, err := Decode([]byte(roundedMark), "image/svg+xml")
	if err != nil {
		t.Fatal(err)
	}
	side := picture.Bounds().Dy()
	scale := float64(side) / 32
	dark := 0
	for y := range side {
		red, _, _, _ := picture.At(picture.Bounds().Dx()/2, y).RGBA()
		if red>>8 < 120 {
			dark++
		}
	}
	want := int(4 * scale)
	if dark < want-2 || dark > want+2 {
		t.Fatalf("линия толщиной 4 при масштабе %.0f заняла %d пикселей, ждали около %d", scale, dark, want)
	}
}

func TestSquareKeepsProportionsAndPadsWithNothing(t *testing.T) {
	wide := image.NewRGBA(image.Rect(0, 0, 100, 20))
	for y := range 20 {
		for x := range 100 {
			wide.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}

	squared := Square(wide, 64)
	if squared.Bounds().Dx() != 64 || squared.Bounds().Dy() != 64 {
		t.Fatalf("вышло %v, ждали квадрат 64", squared.Bounds())
	}
	if !transparent(squared, 32, 1) {
		t.Fatal("над широкой картинкой должно остаться пустое поле, а не растянутое изображение")
	}
	if transparent(squared, 32, 32) {
		t.Fatal("середина должна быть занята картинкой")
	}
}

func TestDecodeReadsRasterAndDataURI(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 8, 8))
	source.Set(4, 4, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(buffer.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 8 {
		t.Fatalf("растр прочитался как %v", decoded.Bounds())
	}

	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
	fromURI, err := FromDataURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if fromURI.Bounds() != decoded.Bounds() {
		t.Fatal("картинка из data URI должна совпадать с исходной")
	}

	if _, err := FromDataURI(""); err == nil {
		t.Fatal("пустая ссылка — это отсутствие картинки, а не картинка")
	}
	if _, err := FromDataURI("https://example/logo.png"); err == nil {
		t.Fatal("обычная ссылка сюда попадать не должна")
	}
}

func TestICOCarriesEverySize(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 64, 64))
	body := ICO(source, TemplateSizes)

	if count := int(body[4]) | int(body[5])<<8; count != len(TemplateSizes) {
		t.Fatalf("в иконке %d картинок, ждали %d", count, len(TemplateSizes))
	}
	biggest := 6 + 16*(len(TemplateSizes)-1)
	declared := int(body[biggest+8]) | int(body[biggest+9])<<8 | int(body[biggest+10])<<16 | int(body[biggest+11])<<24
	if declared < 256*256*4 {
		t.Fatalf("слот 256x256 занимает %d байт — под чужую картинку места не хватит", declared)
	}
}

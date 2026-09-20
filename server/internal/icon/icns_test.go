package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func swatch(width, height int) image.Image {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x40, A: 0xff})
		}
	}
	return picture
}

func TestICNSHoldsEverySizeMacOSAsksFor(t *testing.T) {
	body, err := ICNS(swatch(64, 48))
	if err != nil {
		t.Fatal(err)
	}
	if string(body[:4]) != "icns" {
		t.Fatalf("не тот формат: %q", body[:4])
	}
	if int(binary.BigEndian.Uint32(body[4:8])) != len(body) {
		t.Fatalf("длина в заголовке %d, а файл %d байт", binary.BigEndian.Uint32(body[4:8]), len(body))
	}

	seen := map[string]int{}
	for offset := 8; offset < len(body); {
		if offset+8 > len(body) {
			t.Fatal("запись обрезана")
		}
		kind := string(body[offset : offset+4])
		length := int(binary.BigEndian.Uint32(body[offset+4 : offset+8]))
		if length < 8 || offset+length > len(body) {
			t.Fatalf("запись %s обещает %d байт", kind, length)
		}
		picture, err := png.Decode(bytes.NewReader(body[offset+8 : offset+length]))
		if err != nil {
			t.Fatalf("запись %s — не PNG: %v", kind, err)
		}
		seen[kind] = picture.Bounds().Dx()
		offset += length
	}
	for _, slot := range icnsSlots {
		if seen[slot.kind] != slot.size {
			t.Fatalf("для %s ждали картинку %d пикселей, получили %d", slot.kind, slot.size, seen[slot.kind])
		}
	}
}

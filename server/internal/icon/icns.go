package icon

import (
	"bytes"
	"encoding/binary"
	"image"
)

type icnsSlot struct {
	kind string
	size int
}

var icnsSlots = []icnsSlot{
	{"ic11", 32},
	{"ic12", 64},
	{"ic07", 128},
	{"ic13", 256},
	{"ic08", 256},
	{"ic14", 512},
	{"ic09", 512},
	{"ic10", 1024},
}

func ICNS(source image.Image) ([]byte, error) {
	var body bytes.Buffer
	for _, slot := range icnsSlots {
		picture, err := PNG(Square(source, slot.size))
		if err != nil {
			return nil, err
		}
		body.WriteString(slot.kind)
		binary.Write(&body, binary.BigEndian, uint32(len(picture)+8))
		body.Write(picture)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(body.Len()+8))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

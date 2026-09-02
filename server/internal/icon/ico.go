package icon

import (
	"bytes"
	"encoding/binary"
	"image"
)

var TemplateSizes = []int{16, 24, 32, 48, 64, 128, 256}

func ICO(source image.Image, sizes []int) []byte {
	images := make([][]byte, 0, len(sizes))
	for _, size := range sizes {
		images = append(images, dib(Square(source, size)))
	}

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint16(0))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(len(images)))

	offset := 6 + 16*len(images)
	for i, body := range images {
		side := sizes[i] % 256
		out.Write([]byte{byte(side), byte(side), 0, 0})
		binary.Write(&out, binary.LittleEndian, uint16(1))
		binary.Write(&out, binary.LittleEndian, uint16(32))
		binary.Write(&out, binary.LittleEndian, uint32(len(body)))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(body)
	}
	for _, body := range images {
		out.Write(body)
	}
	return out.Bytes()
}

func dib(picture *image.RGBA) []byte {
	width := picture.Bounds().Dx()
	height := picture.Bounds().Dy()
	maskStride := ((width + 31) / 32) * 4

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint32(40))
	binary.Write(&out, binary.LittleEndian, int32(width))
	binary.Write(&out, binary.LittleEndian, int32(height*2))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(32))
	binary.Write(&out, binary.LittleEndian, uint32(0))
	binary.Write(&out, binary.LittleEndian, uint32(width*height*4+maskStride*height))
	binary.Write(&out, binary.LittleEndian, int32(0))
	binary.Write(&out, binary.LittleEndian, int32(0))
	binary.Write(&out, binary.LittleEndian, uint32(0))
	binary.Write(&out, binary.LittleEndian, uint32(0))

	for y := height - 1; y >= 0; y-- {
		for x := range width {
			pixel := picture.RGBAAt(x, y)
			out.Write([]byte{pixel.B, pixel.G, pixel.R, pixel.A})
		}
	}
	out.Write(make([]byte, maskStride*height))
	return out.Bytes()
}

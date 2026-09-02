package bake

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const (
	magic      = "LAMINARA_CONFIG1"
	trailerLen = 24
	maxConfig  = 8 << 20
)

var (
	ErrEmpty    = errors.New("пустая конфигурация лаунчера")
	ErrTooLarge = errors.New("конфигурация лаунчера длиннее 8 МБ")
)

func Attach(image, config []byte) ([]byte, error) {
	if len(config) == 0 {
		return nil, ErrEmpty
	}
	if len(config) > maxConfig {
		return nil, ErrTooLarge
	}
	stripped := Strip(image)
	out := make([]byte, 0, len(stripped)+len(config)+trailerLen)
	out = append(out, stripped...)
	out = append(out, config...)
	out = binary.LittleEndian.AppendUint64(out, uint64(len(config)))
	out = append(out, magic...)
	return out, nil
}

func Read(image []byte) ([]byte, bool) {
	start, length, ok := locate(image)
	if !ok {
		return nil, false
	}
	return image[start : start+length], true
}

func Strip(image []byte) []byte {
	start, _, ok := locate(image)
	if !ok {
		return image
	}
	return image[:start]
}

func Baked(image []byte) bool {
	_, _, ok := locate(image)
	return ok
}

func Holds(image, config []byte) bool {
	current, ok := Read(image)
	return ok && bytes.Equal(current, config)
}

func locate(image []byte) (start, length int, ok bool) {
	if len(image) <= trailerLen {
		return 0, 0, false
	}
	trailer := image[len(image)-trailerLen:]
	if string(trailer[8:]) != magic {
		return 0, 0, false
	}
	declared := binary.LittleEndian.Uint64(trailer[:8])
	if declared == 0 || declared > uint64(len(image)-trailerLen) {
		return 0, 0, false
	}
	size := int(declared)
	return len(image) - trailerLen - size, size, true
}

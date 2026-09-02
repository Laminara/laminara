package winres

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"testing"
)

func candidate(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("LAMINARA_WINRES_EXE")
	if path == "" {
		t.Skip("нет LAMINARA_WINRES_EXE — тесту нужен собранный лаунчер под Windows")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func logo() image.Image {
	picture := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			picture.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 8), B: 200, A: 255})
		}
	}
	return picture
}

func TestSetIconRewritesEveryIconSlot(t *testing.T) {
	original := candidate(t)

	painted, err := SetIcon(original, logo())
	if err != nil {
		t.Fatal(err)
	}
	if len(painted) != len(original) {
		t.Fatalf("замена иконки не должна менять размер файла: было %d, стало %d", len(original), len(painted))
	}
	if bytes.Equal(painted, original) {
		t.Fatal("файл не изменился — иконка не заменилась")
	}

	before, err := parse(original)
	if err != nil {
		t.Fatal(err)
	}
	after, err := parse(painted)
	if err != nil {
		t.Fatal(err)
	}
	oldSlots, err := before.slots(typeIcon)
	if err != nil {
		t.Fatal(err)
	}
	newSlots, err := after.slots(typeIcon)
	if err != nil {
		t.Fatal(err)
	}
	if len(newSlots) == 0 || len(newSlots) != len(oldSlots) {
		t.Fatalf("слотов иконок было %d, стало %d", len(oldSlots), len(newSlots))
	}

	pngSignature := []byte{0x89, 'P', 'N', 'G'}
	for i, entry := range newSlots {
		body := painted[entry.payload : entry.payload+entry.capacity]
		if !bytes.HasPrefix(body, pngSignature) {
			t.Fatalf("слот %d не получил картинку", i)
		}
		if entry.capacity > oldSlots[i].capacity {
			t.Fatalf("слот %d вырос с %d до %d — так делать нельзя", i, oldSlots[i].capacity, entry.capacity)
		}
	}
}

func TestSetIconUpdatesTheGroupTable(t *testing.T) {
	painted, err := SetIcon(candidate(t), logo())
	if err != nil {
		t.Fatal(err)
	}
	after, err := parse(painted)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := after.slots(typeGroupIcon)
	if err != nil {
		t.Fatal(err)
	}
	icons, err := after.slots(typeIcon)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[uint32]int{}
	for _, entry := range icons {
		sizes[entry.id] = entry.capacity
	}

	for _, group := range groups {
		header := painted[group.payload : group.payload+group.capacity]
		count := int(binary.LittleEndian.Uint16(header[4:6]))
		for i := range count {
			entry := header[groupHeaderLen+i*groupEntryLen:]
			id := uint32(binary.LittleEndian.Uint16(entry[12:14]))
			declared := binary.LittleEndian.Uint32(entry[8:12])
			if int(declared) != sizes[id] {
				t.Fatalf("оглавление обещает %d байт для картинки %d, а в ресурсе %d", declared, id, sizes[id])
			}
		}
	}
}

func TestSetIconRefusesSomethingThatIsNotAnExecutable(t *testing.T) {
	if _, err := SetIcon([]byte("just some bytes"), logo()); err == nil {
		t.Fatal("не-исполняемый файл должен быть отвергнут")
	}
}

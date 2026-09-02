package winres

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"image"

	"github.com/laminara/laminara/server/internal/icon"
)

const (
	typeIcon      = 3
	typeGroupIcon = 14

	directoryHeaderLen = 16
	directoryEntryLen  = 8
	dataEntryLen       = 16
	groupHeaderLen     = 6
	groupEntryLen      = 14
	subdirectoryFlag   = 0x80000000
)

var (
	ErrNoResources = errors.New("в файле нет ресурсов Windows")
	ErrNoIcon      = errors.New("в файле нет иконки, которую можно заменить")
)

type slot struct {
	id        uint32
	dataEntry int
	payload   int
	capacity  int
}

type layout struct {
	image     []byte
	base      int
	baseRVA   uint32
	sectionAt int
	sectionAd uint32
}

func SetIcon(binaryImage []byte, logo image.Image) ([]byte, error) {
	out := append([]byte(nil), binaryImage...)
	found, err := parse(out)
	if err != nil {
		return nil, err
	}

	icons, err := found.slots(typeIcon)
	if err != nil {
		return nil, err
	}
	groups, err := found.slots(typeGroupIcon)
	if err != nil {
		return nil, err
	}
	if len(icons) == 0 || len(groups) == 0 {
		return nil, ErrNoIcon
	}

	byID := make(map[uint32]slot, len(icons))
	for _, entry := range icons {
		byID[entry.id] = entry
	}

	for _, group := range groups {
		if err := found.repaintGroup(group, byID, logo); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (l *layout) repaintGroup(group slot, icons map[uint32]slot, logo image.Image) error {
	header := l.image[group.payload : group.payload+group.capacity]
	if len(header) < groupHeaderLen {
		return fmt.Errorf("оглавление иконок короче заголовка")
	}
	count := int(binary.LittleEndian.Uint16(header[4:6]))
	if groupHeaderLen+count*groupEntryLen > len(header) {
		return fmt.Errorf("оглавление иконок обещает %d картинок, но столько не помещается", count)
	}

	for i := range count {
		entry := header[groupHeaderLen+i*groupEntryLen:]
		size := int(entry[0])
		if size == 0 {
			size = 256
		}
		id := uint32(binary.LittleEndian.Uint16(entry[12:14]))
		target, ok := icons[id]
		if !ok {
			continue
		}

		encoded, err := icon.PNG(icon.Square(logo, size))
		if err != nil {
			return err
		}
		if len(encoded) > target.capacity {
			return fmt.Errorf("картинка %dx%d занимает %d байт, а в шаблоне под неё %d — возьмите картинку проще", size, size, len(encoded), target.capacity)
		}

		copy(l.image[target.payload:target.payload+target.capacity], make([]byte, target.capacity))
		copy(l.image[target.payload:], encoded)
		binary.LittleEndian.PutUint32(l.image[target.dataEntry+4:], uint32(len(encoded)))
		binary.LittleEndian.PutUint32(entry[8:12], uint32(len(encoded)))
	}
	return nil
}

func parse(binaryImage []byte) (*layout, error) {
	file, err := pe.NewFile(bytes.NewReader(binaryImage))
	if err != nil {
		return nil, fmt.Errorf("это не исполняемый файл Windows: %w", err)
	}
	defer file.Close()

	var directory pe.DataDirectory
	switch header := file.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		directory = header.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE]
	case *pe.OptionalHeader32:
		directory = header.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE]
	default:
		return nil, fmt.Errorf("незнакомый заголовок PE")
	}
	if directory.VirtualAddress == 0 || directory.Size == 0 {
		return nil, ErrNoResources
	}

	for _, section := range file.Sections {
		if directory.VirtualAddress < section.VirtualAddress ||
			directory.VirtualAddress >= section.VirtualAddress+section.VirtualSize {
			continue
		}
		base := int(section.Offset + (directory.VirtualAddress - section.VirtualAddress))
		if base >= len(binaryImage) {
			return nil, fmt.Errorf("таблица ресурсов указывает за пределы файла")
		}
		return &layout{
			image:     binaryImage,
			base:      base,
			baseRVA:   directory.VirtualAddress,
			sectionAt: int(section.Offset),
			sectionAd: section.VirtualAddress,
		}, nil
	}
	return nil, ErrNoResources
}

func (l *layout) slots(kind uint32) ([]slot, error) {
	typeEntry, ok, err := l.child(l.base, kind)
	if err != nil || !ok {
		return nil, err
	}
	names, err := l.entries(typeEntry)
	if err != nil {
		return nil, err
	}

	var found []slot
	for _, name := range names {
		if !name.directory {
			continue
		}
		languages, err := l.entries(l.base + name.offset)
		if err != nil {
			return nil, err
		}
		for _, language := range languages {
			if language.directory {
				continue
			}
			at := l.base + language.offset
			if at+dataEntryLen > len(l.image) {
				return nil, fmt.Errorf("ресурс указывает за пределы файла")
			}
			rva := binary.LittleEndian.Uint32(l.image[at:])
			size := binary.LittleEndian.Uint32(l.image[at+4:])
			payload := l.offsetOf(rva)
			if payload < 0 || payload+int(size) > len(l.image) {
				return nil, fmt.Errorf("данные ресурса лежат за пределами файла")
			}
			found = append(found, slot{id: name.id, dataEntry: at, payload: payload, capacity: int(size)})
		}
	}
	return found, nil
}

type entry struct {
	id        uint32
	offset    int
	directory bool
}

func (l *layout) entries(at int) ([]entry, error) {
	if at+directoryHeaderLen > len(l.image) {
		return nil, fmt.Errorf("каталог ресурсов выходит за пределы файла")
	}
	named := int(binary.LittleEndian.Uint16(l.image[at+12:]))
	ids := int(binary.LittleEndian.Uint16(l.image[at+14:]))
	total := named + ids
	start := at + directoryHeaderLen
	if start+total*directoryEntryLen > len(l.image) {
		return nil, fmt.Errorf("каталог ресурсов обещает больше записей, чем помещается")
	}

	out := make([]entry, 0, total)
	for i := range total {
		record := l.image[start+i*directoryEntryLen:]
		name := binary.LittleEndian.Uint32(record)
		offset := binary.LittleEndian.Uint32(record[4:])
		out = append(out, entry{
			id:        name,
			offset:    int(offset &^ subdirectoryFlag),
			directory: offset&subdirectoryFlag != 0,
		})
	}
	return out, nil
}

func (l *layout) child(at int, id uint32) (int, bool, error) {
	children, err := l.entries(at)
	if err != nil {
		return 0, false, err
	}
	for _, child := range children {
		if child.id == id && child.directory {
			return l.base + child.offset, true, nil
		}
	}
	return 0, false, nil
}

func (l *layout) offsetOf(rva uint32) int {
	if rva < l.sectionAd {
		return -1
	}
	return l.sectionAt + int(rva-l.sectionAd)
}

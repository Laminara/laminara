package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/laminara/laminara/server/internal/icon"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "как пользоваться: iconslots <картинка> <иконка.ico | картинка.png> [размер]")
		os.Exit(2)
	}
	source, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	picture, err := icon.Decode(source, "")
	if err != nil {
		fail(err)
	}

	target := os.Args[2]
	if strings.HasSuffix(strings.ToLower(target), ".png") {
		side := 512
		if len(os.Args) > 3 {
			side, err = strconv.Atoi(os.Args[3])
			if err != nil {
				fail(err)
			}
		}
		body, err := icon.PNG(icon.Square(picture, side))
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("%s: %dx%d, %d байт\n", target, side, side, len(body))
		return
	}

	body := icon.ICO(picture, icon.TemplateSizes)
	if err := os.WriteFile(target, body, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("%s: %d слотов, %d байт\n", target, len(icon.TemplateSizes), len(body))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

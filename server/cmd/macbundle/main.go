package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/laminara/laminara/server/internal/clientconfig"
	"github.com/laminara/laminara/server/internal/macapp"
)

func main() {
	config := flag.String("config", "", "файл настроек лаунчера от client-config")
	binary := flag.String("binary", "", "собранный лаунчер для macOS")
	out := flag.String("out", ".", "куда положить .app")
	name := flag.String("name", "", "как назвать приложение (по умолчанию — из оформления)")
	release := flag.String("version", "0.0.0", "версия лаунчера")
	flag.Parse()

	if *config == "" || *binary == "" {
		fmt.Fprintln(os.Stderr, "нужны --config и --binary")
		os.Exit(2)
	}
	payload, err := os.ReadFile(*config)
	if err != nil {
		fail(err)
	}
	var document clientconfig.Document
	if err := json.Unmarshal(payload, &document); err != nil {
		fail(fmt.Errorf("%s не разобрать как настройки лаунчера: %w", *config, err))
	}
	executable, err := os.ReadFile(*binary)
	if err != nil {
		fail(err)
	}
	if *name == "" {
		*name = document.LauncherName()
	}
	picture, err := macapp.Icon(brandingLogo(document))
	if err != nil {
		fmt.Fprintf(os.Stderr, "иконку взять не вышло, оставляю стандартную: %v\n", err)
	}

	bundle := macapp.Bundle{
		Name:       *name,
		Version:    *release,
		Executable: executable,
		Icon:       picture,
		Config:     payload,
	}
	path, err := bundle.Write(*out)
	if err != nil {
		fail(err)
	}
	fmt.Println(path)
}

func brandingLogo(document clientconfig.Document) string {
	if document.Branding == nil {
		return ""
	}
	return document.Branding.LogoDataURI
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

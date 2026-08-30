package mediatype

import "testing"

func TestGuess(t *testing.T) {
	tests := []struct {
		name     string
		declared string
		want     string
	}{
		{"logo.png", "", "image/png"},
		{"hero.MP4", "", "video/mp4"},
		{"https://cdn.example.com/banner.jpg?v=2", "", "image/jpeg"},
		{"/srv/news/today", "", Default},
		{"", "", Default},
		{"/srv/news/today", "image/webp", "image/webp"},
		{"/srv/news/today", "Image/JPEG; charset=binary", "image/jpeg"},
		{"/srv/hero.mp4", "text/plain", "video/mp4"},
		{"/srv/news/today", "application/json", Default},
		{"", "video/webm", "video/webm"},
	}
	for _, test := range tests {
		if got := Guess(test.name, test.declared); got != test.want {
			t.Fatalf("Guess(%q, %q) = %q, want %q", test.name, test.declared, got, test.want)
		}
	}
}

func TestGuessImage(t *testing.T) {
	tests := []struct {
		name     string
		declared string
		want     string
	}{
		{"logo.png", "", "image/png"},
		{"https://cdn.example.com/banner.jpg?v=2", "", "image/jpeg"},
		{"/srv/news/today", "image/webp", "image/webp"},
		{"/srv/news/today", "Image/JPEG; charset=binary", "image/jpeg"},
		{"/srv/news/today", "application/octet-stream", DefaultImage},
		{"/srv/news/today", "", DefaultImage},
		{"", "avif", DefaultImage},
		{"/srv/clip.mp4", "", DefaultImage},
		{"/srv/clip.mp4", "video/mp4", DefaultImage},
	}
	for _, test := range tests {
		if got := GuessImage(test.name, test.declared); got != test.want {
			t.Fatalf("GuessImage(%q, %q) = %q, want %q", test.name, test.declared, got, test.want)
		}
	}
}

package humanize

import (
	"testing"
	"time"
)

func TestBytes(t *testing.T) {
	cases := map[uint64]string{
		0:             "0 Б",
		512:           "512 Б",
		1024:          "1,0 КБ",
		1536:          "1,5 КБ",
		1019954837:    "973 МБ",
		5 * 1 << 30:   "5,0 ГБ",
		1 << 40:       "1,0 ТБ",
		1024*1024 - 1: "1024 КБ",
	}
	for size, want := range cases {
		if got := Bytes(size); got != want {
			t.Fatalf("Bytes(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestCount(t *testing.T) {
	cases := map[int]string{1: "1 файл", 2: "2 файла", 5: "5 файлов", 11: "11 файлов", 21: "21 файл", 104: "104 файла"}
	for value, want := range cases {
		if got := Count(value, "файл", "файла", "файлов"); got != want {
			t.Fatalf("Count(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		3 * time.Second:                  "3 с",
		90 * time.Second:                 "1 мин 30 с",
		time.Hour + 5*time.Minute:        "1 ч 05 мин",
		2*time.Hour + 30*time.Minute + 1: "2 ч 30 мин",
		90 * 24 * time.Hour:              "90 дней",
		72 * time.Hour:                   "3 дня",
		24 * time.Hour:                   "24 ч",
		10 * time.Minute:                 "10 мин",
	}
	for d, want := range cases {
		if got := Duration(d); got != want {
			t.Fatalf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestWhen(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.Local)
	cases := map[time.Time]string{
		time.Date(2026, 8, 25, 9, 5, 0, 0, time.Local):    "сегодня в 09:05",
		time.Date(2026, 8, 24, 22, 40, 0, 0, time.Local):  "вчера в 22:40",
		time.Date(2026, 8, 3, 8, 0, 0, 0, time.Local):     "3 августа в 08:00",
		time.Date(2025, 12, 31, 23, 59, 0, 0, time.Local): "31 декабря 2025 в 23:59",
	}
	for moment, want := range cases {
		if got := whenAt(moment, now); got != want {
			t.Fatalf("whenAt(%s) = %q; want %q", moment, got, want)
		}
	}
	if got := whenAt(time.Time{}, now); got != "неизвестно когда" {
		t.Fatalf("zero time = %q", got)
	}
}

package humanize

import (
	"fmt"
	"strings"
	"time"
)

var units = []string{"Б", "КБ", "МБ", "ГБ", "ТБ"}

func Bytes(size uint64) string {
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", size, units[unit])
	}
	if value >= 100 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return strings.Replace(fmt.Sprintf("%.1f %s", value, units[unit]), ".", ",", 1)
}

func Count(value int, one, few, many string) string {
	mod100 := value % 100
	mod10 := mod100 % 10
	word := many
	switch {
	case mod100 >= 11 && mod100 <= 14:
		word = many
	case mod10 == 1:
		word = one
	case mod10 >= 2 && mod10 <= 4:
		word = few
	}
	return fmt.Sprintf("%d %s", value, word)
}

func Duration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%d с", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d мин %02d с", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%d ч %02d мин", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	return Count(days, "день", "дня", "дней")
}

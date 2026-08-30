package duration

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

func (d Duration) Compact() string {
	value := time.Duration(d)
	if value == 0 {
		return "0s"
	}
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	}
	if value%time.Second != 0 {
		return sign + value.String()
	}
	parts := ""
	if hours := int64(value / time.Hour); hours > 0 {
		parts += strconv.FormatInt(hours, 10) + "h"
	}
	if minutes := int64(value/time.Minute) % 60; minutes > 0 {
		parts += strconv.FormatInt(minutes, 10) + "m"
	}
	if seconds := int64(value/time.Second) % 60; seconds > 0 {
		parts += strconv.FormatInt(seconds, 10) + "s"
	}
	return sign + parts
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.Compact()) }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := Parse(value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func Parse(value string) (Duration, error) {
	text := strings.TrimSpace(value)
	if days, ok := strings.CutSuffix(text, "d"); ok {
		number, err := strconv.Atoi(days)
		if err != nil {
			return 0, invalid(value)
		}
		return Duration(time.Duration(number) * 24 * time.Hour), nil
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return 0, invalid(value)
	}
	return Duration(parsed), nil
}

func invalid(value string) error {
	return errors.New("пишите время как 30s, 15m, 12h или 30d, а не «" + value + "»")
}

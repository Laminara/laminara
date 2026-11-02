package command

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func Split(line string) ([]string, error) {
	var arguments []string
	for line = strings.TrimSpace(line); line != ""; line = strings.TrimSpace(line) {
		if line[0] == '"' || line[0] == '\'' {
			quote := line[0]
			end := 1
			for ; end < len(line); end++ {
				if quote == '"' && line[end] == '\\' {
					end++
					continue
				}
				if line[end] == quote {
					break
				}
			}
			if end >= len(line) {
				return nil, fmt.Errorf("закройте кавычки в аргументе команды")
			}
			word := line[1:end]
			if quote == '"' {
				decoded, err := strconv.Unquote(line[:end+1])
				if err != nil {
					return nil, fmt.Errorf("неверное экранирование в аргументе команды")
				}
				word = decoded
			}
			line = line[end+1:]
			if !argumentBoundary(line) {
				return nil, fmt.Errorf("после кавычек нужен пробел")
			}
			arguments = append(arguments, word)
			continue
		}
		if line[0] == '{' || line[0] == '[' {
			decoder := json.NewDecoder(strings.NewReader(line))
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err == nil && argumentBoundary(line[decoder.InputOffset():]) {
				arguments = append(arguments, line[:decoder.InputOffset()])
				line = line[decoder.InputOffset():]
				continue
			}
		}
		end := strings.IndexFunc(line, unicode.IsSpace)
		if end < 0 {
			arguments = append(arguments, line)
			break
		}
		arguments = append(arguments, line[:end])
		line = line[end:]
	}
	return arguments, nil
}

func argumentBoundary(value string) bool {
	if value == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(value)
	return unicode.IsSpace(first)
}

func Format(arguments ...string) string {
	words := make([]string, len(arguments))
	for index, word := range arguments {
		if word == "" || strings.ContainsAny(word, "\"'\\") || strings.IndexFunc(word, unicode.IsSpace) >= 0 {
			word = strconv.Quote(word)
		}
		words[index] = word
	}
	return strings.Join(words, " ")
}

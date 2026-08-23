package access

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Roster struct {
	global   map[string]struct{}
	perBuild map[string]map[string]struct{}
}

type rosterDocument struct {
	Users   []string                   `json:"users"`
	UUIDs   []string                   `json:"uuids"`
	Members []string                   `json:"members"`
	Builds  map[string]json.RawMessage `json:"builds"`
}

func normalizeMember(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
}

func memberSet(values ...[]string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, list := range values {
		for _, value := range list {
			if normalized := normalizeMember(value); normalized != "" {
				set[normalized] = struct{}{}
			}
		}
	}
	return set
}

func parseMembers(raw json.RawMessage) (map[string]struct{}, error) {
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return memberSet(list), nil
	}
	var doc rosterDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return memberSet(doc.Users, doc.UUIDs, doc.Members), nil
}

func ParseRoster(data []byte) (*Roster, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return &Roster{global: map[string]struct{}{}}, nil
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return parseRosterLines(trimmed), nil
	}

	roster := &Roster{global: map[string]struct{}{}}
	if trimmed[0] == '[' {
		members, err := parseMembers(trimmed)
		if err != nil {
			return nil, err
		}
		roster.global = members
		return roster, nil
	}

	var doc rosterDocument
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return nil, fmt.Errorf("whitelist is not valid JSON: %w", err)
	}
	roster.global = memberSet(doc.Users, doc.UUIDs, doc.Members)
	if len(doc.Builds) > 0 {
		roster.perBuild = make(map[string]map[string]struct{}, len(doc.Builds))
		for build, raw := range doc.Builds {
			members, err := parseMembers(raw)
			if err != nil {
				return nil, fmt.Errorf("whitelist for build %q: %w", build, err)
			}
			roster.perBuild[build] = members
		}
	}
	return roster, nil
}

func parseRosterLines(data []byte) *Roster {
	members := make(map[string]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if index := strings.IndexByte(line, '#'); index >= 0 {
			line = line[:index]
		}
		if normalized := normalizeMember(line); normalized != "" {
			members[normalized] = struct{}{}
		}
	}
	return &Roster{global: members}
}

func (r *Roster) Contains(build string, subject Subject) bool {
	if r == nil {
		return false
	}
	members := r.global
	if scoped, ok := r.perBuild[build]; ok {
		members = scoped
	}
	if len(members) == 0 {
		return false
	}
	for _, candidate := range []string{subject.Username, subject.UUID, subject.Subject} {
		if candidate == "" {
			continue
		}
		if _, ok := members[normalizeMember(candidate)]; ok {
			return true
		}
	}
	return false
}

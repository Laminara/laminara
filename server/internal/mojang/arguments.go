package mojang

import "encoding/json"

type Argument struct {
	Rules  []Rule
	Values []string
}

type Arguments struct {
	Game []Argument `json:"game"`
	JVM  []Argument `json:"jvm"`
}

func (a *Argument) UnmarshalJSON(data []byte) error {
	var literal string
	if err := json.Unmarshal(data, &literal); err == nil {
		a.Rules = nil
		a.Values = []string{literal}
		return nil
	}
	var conditional struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &conditional); err != nil {
		return err
	}
	a.Rules = conditional.Rules
	if len(conditional.Value) == 0 {
		a.Values = nil
		return nil
	}
	var many []string
	if err := json.Unmarshal(conditional.Value, &many); err == nil {
		a.Values = many
		return nil
	}
	var one string
	if err := json.Unmarshal(conditional.Value, &one); err != nil {
		a.Values = nil
		return nil
	}
	a.Values = []string{one}
	return nil
}

func Selected(arguments []Argument, os, arch string) []string {
	values := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if !EvaluateRules(argument.Rules, os, arch) {
			continue
		}
		values = append(values, argument.Values...)
	}
	return values
}

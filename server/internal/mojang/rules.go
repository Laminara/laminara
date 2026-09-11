package mojang

func EvaluateRules(rules []Rule, os, arch string) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range rules {
		if ruleMatches(rule, os, arch) {
			allowed = rule.Action == "allow"
		}
	}
	return allowed
}

func ruleMatches(rule Rule, os, arch string) bool {
	if len(rule.Features) > 0 {
		return false
	}
	if rule.OS == nil {
		return true
	}
	if rule.OS.Version != "" {
		return false
	}
	if rule.OS.Name != "" && rule.OS.Name != os {
		return false
	}
	if rule.OS.Arch != "" && rule.OS.Arch != arch {
		return false
	}
	return true
}

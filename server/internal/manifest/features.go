package manifest

import (
	"fmt"
	"strings"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

func featureModelFromSpec(spec *FeatureSpec) *corev1.FeatureModel {
	if spec == nil || len(spec.Groups) == 0 {
		return nil
	}
	return &corev1.FeatureModel{Groups: groupsFromSpec(spec.Groups)}
}

func groupsFromSpec(groups []GroupSpec) []*corev1.FeatureGroup {
	out := make([]*corev1.FeatureGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, &corev1.FeatureGroup{
			Id:          g.ID,
			Title:       g.Title,
			Description: g.Description,
			Selection:   selectionFromString(g.Selection),
			Required:    g.Required,
			Options:     optionsFromSpec(g.Options),
		})
	}
	return out
}

func optionsFromSpec(options []OptionSpec) []*corev1.FeatureOption {
	out := make([]*corev1.FeatureOption, 0, len(options))
	for _, o := range options {
		option := &corev1.FeatureOption{
			Id:             o.ID,
			Title:          o.Title,
			Description:    o.Description,
			DefaultEnabled: o.DefaultEnabled,
			Files:          o.Files,
			Groups:         groupsFromSpec(o.Groups),
		}
		if o.Meta != nil {
			option.Meta = &corev1.OptionMeta{
				Icon:             o.Meta.Icon,
				Badge:            o.Meta.Badge,
				Requires:         o.Meta.Requires,
				IncompatibleWith: o.Meta.IncompatibleWith,
			}
		}
		out = append(out, option)
	}
	return out
}

func selectionFromString(s string) corev1.SelectionType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "single":
		return corev1.SelectionType_SELECTION_TYPE_SINGLE
	case "multi":
		return corev1.SelectionType_SELECTION_TYPE_MULTI
	default:
		return corev1.SelectionType_SELECTION_TYPE_UNSPECIFIED
	}
}

func validateFeatures(model *corev1.FeatureModel, policyByPath map[string]corev1.FilePolicy) error {
	if model == nil {
		return nil
	}
	if err := validateGroups(model.Groups, policyByPath); err != nil {
		return err
	}
	known := make(map[string]bool)
	collectOptionAddresses(model.Groups, "", known)
	return validateConstraints(model.Groups, "", known)
}

func collectOptionAddresses(groups []*corev1.FeatureGroup, parentAddr string, known map[string]bool) {
	for _, g := range groups {
		groupAddr := g.Id
		if parentAddr != "" {
			groupAddr = parentAddr + "/" + g.Id
		}
		for _, o := range g.Options {
			optionAddr := groupAddr + "#" + o.Id
			known[optionAddr] = true
			collectOptionAddresses(o.Groups, optionAddr, known)
		}
	}
}

func validateConstraints(groups []*corev1.FeatureGroup, parentAddr string, known map[string]bool) error {
	for _, g := range groups {
		groupAddr := g.Id
		if parentAddr != "" {
			groupAddr = parentAddr + "/" + g.Id
		}
		for _, o := range g.Options {
			optionAddr := groupAddr + "#" + o.Id
			if o.Meta != nil {
				for _, ref := range append(append([]string{}, o.Meta.Requires...), o.Meta.IncompatibleWith...) {
					if ref == optionAddr {
						return fmt.Errorf("option %q references itself in requires/incompatibleWith", optionAddr)
					}
					if !known[ref] {
						return fmt.Errorf("option %q references unknown option address %q", optionAddr, ref)
					}
				}
			}
			if err := validateConstraints(o.Groups, optionAddr, known); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateGroups(groups []*corev1.FeatureGroup, policyByPath map[string]corev1.FilePolicy) error {
	seen := make(map[string]bool, len(groups))
	for _, g := range groups {
		if g.Id == "" {
			return fmt.Errorf("feature group has an empty id")
		}
		if seen[g.Id] {
			return fmt.Errorf("duplicate feature group id %q", g.Id)
		}
		seen[g.Id] = true
		if g.Selection == corev1.SelectionType_SELECTION_TYPE_UNSPECIFIED {
			return fmt.Errorf("feature group %q: selection must be \"single\" or \"multi\"", g.Id)
		}
		if len(g.Options) == 0 {
			return fmt.Errorf("feature group %q has no options", g.Id)
		}
		defaults := 0
		optSeen := make(map[string]bool, len(g.Options))
		for _, o := range g.Options {
			if o.Id == "" {
				return fmt.Errorf("feature group %q: option with an empty id", g.Id)
			}
			if optSeen[o.Id] {
				return fmt.Errorf("feature group %q: duplicate option id %q", g.Id, o.Id)
			}
			optSeen[o.Id] = true
			if o.DefaultEnabled {
				defaults++
			}
			for _, file := range o.Files {
				policy, ok := policyByPath[file]
				if !ok {
					return fmt.Errorf("option %q/%q: file %q is not in the build", g.Id, o.Id, file)
				}
				if policy == corev1.FilePolicy_FILE_POLICY_USER_WRITABLE {
					return fmt.Errorf("option %q/%q: file %q is user_writable; optional files must be immutable or enforced", g.Id, o.Id, file)
				}
			}
			if err := validateGroups(o.Groups, policyByPath); err != nil {
				return err
			}
		}
		if g.Selection == corev1.SelectionType_SELECTION_TYPE_SINGLE && defaults > 1 {
			return fmt.Errorf("single group %q: at most one option may be defaultEnabled", g.Id)
		}
	}
	return nil
}

func computeAddedSizes(groups []*corev1.FeatureGroup, sizeByPath map[string]uint64) {
	for _, g := range groups {
		for _, o := range g.Options {
			var sum uint64
			for _, file := range o.Files {
				sum += sizeByPath[file]
			}
			if o.Meta == nil {
				o.Meta = &corev1.OptionMeta{}
			}
			o.Meta.AddedSize = sum
			computeAddedSizes(o.Groups, sizeByPath)
		}
	}
}

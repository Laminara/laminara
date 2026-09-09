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
			JvmArgs:        o.JvmArgs,
			GameArgs:       o.GameArgs,
			Classpath:      o.Classpath,
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
						return fmt.Errorf("вариант «%s» ссылается сам на себя в requires/incompatibleWith", optionAddr)
					}
					if !known[ref] {
						return fmt.Errorf("вариант «%s» ссылается на «%s», а такого варианта нет", optionAddr, ref)
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
			return fmt.Errorf("у группы модов пустой id")
		}
		if seen[g.Id] {
			return fmt.Errorf("группа модов «%s» описана дважды", g.Id)
		}
		seen[g.Id] = true
		if g.Selection == corev1.SelectionType_SELECTION_TYPE_UNSPECIFIED {
			return fmt.Errorf("группа «%s»: selection бывает только \"single\" или \"multi\"", g.Id)
		}
		if len(g.Options) == 0 {
			return fmt.Errorf("в группе «%s» нет ни одного варианта", g.Id)
		}
		defaults := 0
		optSeen := make(map[string]bool, len(g.Options))
		for _, o := range g.Options {
			if o.Id == "" {
				return fmt.Errorf("в группе «%s» есть вариант с пустым id", g.Id)
			}
			if optSeen[o.Id] {
				return fmt.Errorf("в группе «%s» вариант «%s» описан дважды", g.Id, o.Id)
			}
			optSeen[o.Id] = true
			if o.DefaultEnabled {
				defaults++
			}
			for _, file := range o.Files {
				policy, ok := policyByPath[file]
				if !ok {
					return fmt.Errorf("вариант «%s/%s»: файла «%s» в сборке нет", g.Id, o.Id, file)
				}
				if policy == corev1.FilePolicy_FILE_POLICY_USER_WRITABLE {
					return fmt.Errorf("вариант «%s/%s»: файл «%s» помечен как изменяемый игроком — необязательные файлы должны быть неизменяемыми", g.Id, o.Id, file)
				}
			}
			if err := validateLaunchArgs(g.Id, o, policyByPath); err != nil {
				return err
			}
			if err := validateGroups(o.Groups, policyByPath); err != nil {
				return err
			}
		}
		if g.Selection == corev1.SelectionType_SELECTION_TYPE_SINGLE && defaults > 1 {
			return fmt.Errorf("в группе «%s» с одиночным выбором включённым по умолчанию может быть только один вариант", g.Id)
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

var reservedJvmArgs = map[string]bool{
	"-cp":                 true,
	"-classpath":          true,
	"--class-path":        true,
	"-Djava.class.path":   true,
	"-Djava.library.path": true,
	"-Dorg.lwjgl.system.SharedLibraryExtractPath": true,
	"-Dminecraft.launcher.brand":                  true,
	"-Dminecraft.launcher.version":                true,
	"-Dauthlibinjector.side":                      true,
	"-Dauthlibinjector.yggdrasil.prefetched":      true,
}

var reservedGameArgs = map[string]bool{
	"--gameDir":     true,
	"--assetsDir":   true,
	"--assetIndex":  true,
	"--accessToken": true,
	"--uuid":        true,
	"--username":    true,
	"--version":     true,
	"--versionType": true,
	"--userType":    true,
	"--clientId":    true,
	"--xuid":        true,
}

var valuedJvmArgs = map[string]bool{
	"--add-opens":            true,
	"--add-exports":          true,
	"--add-reads":            true,
	"--add-modules":          true,
	"--patch-module":         true,
	"--enable-native-access": true,
	"--limit-modules":        true,
}

func checkJvmArgs(where string, args []string) error {
	expectsValue := false
	for _, arg := range args {
		if expectsValue {
			expectsValue = false
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return fmt.Errorf("%s: jvmArgs entry %q is not a java option — it would be taken as the main class", where, arg)
		}
		key := arg
		if i := strings.IndexAny(arg, "=:"); i > 0 {
			key = arg[:i]
		}
		if reservedJvmArgs[key] {
			return fmt.Errorf("%s: jvmArgs must not set %q — the launcher builds the classpath from the profile and the chosen options", where, key)
		}
		expectsValue = valuedJvmArgs[arg]
	}
	if expectsValue {
		return fmt.Errorf("%s: jvmArgs ends with %q and no value after it", where, args[len(args)-1])
	}
	return nil
}

func checkGameArgs(where string, args []string) error {
	for _, arg := range args {
		key := arg
		if i := strings.Index(arg, "="); i > 0 {
			key = arg[:i]
		}
		if reservedGameArgs[key] {
			return fmt.Errorf("%s: gameArgs must not set %q — the launcher passes it itself", where, key)
		}
	}
	return nil
}

func validateBuildArgs(jvmArgs, gameArgs, classpath []string) error {
	if err := checkJvmArgs("сборка", jvmArgs); err != nil {
		return err
	}
	if err := checkGameArgs("сборка", gameArgs); err != nil {
		return err
	}
	for _, entry := range classpath {
		if err := validateClasspathEntry(entry); err != nil {
			return fmt.Errorf("сборка: %w", err)
		}
	}
	return nil
}

func validateLaunchArgs(groupID string, o *corev1.FeatureOption, policyByPath map[string]corev1.FilePolicy) error {
	where := fmt.Sprintf("option %q/%q", groupID, o.Id)
	if err := checkJvmArgs(where, o.JvmArgs); err != nil {
		return err
	}
	if err := checkGameArgs(where, o.GameArgs); err != nil {
		return err
	}
	for _, entry := range o.Classpath {
		if err := validateClasspathEntry(entry); err != nil {
			return fmt.Errorf("вариант «%s/%s»: %w", groupID, o.Id, err)
		}
		if _, ok := policyByPath[entry]; !ok {
			return fmt.Errorf("вариант «%s/%s»: в classpath указан «%s», которого в сборке нет", groupID, o.Id, entry)
		}
	}
	return nil
}

func validateClasspathEntry(entry string) error {
	if entry == "" {
		return fmt.Errorf("в classpath пустая строка")
	}
	if strings.ContainsAny(entry, ":;\\") || strings.HasPrefix(entry, "/") {
		return fmt.Errorf("в classpath «%s» — нужен путь внутри сборки через прямые слэши", entry)
	}
	for _, segment := range strings.Split(entry, "/") {
		if segment == ".." {
			return fmt.Errorf("в classpath «%s» ведёт за пределы сборки", entry)
		}
	}
	return nil
}

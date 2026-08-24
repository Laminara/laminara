package manifest

import (
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

func policyMap(paths ...string) map[string]corev1.FilePolicy {
	m := make(map[string]corev1.FilePolicy, len(paths))
	for _, p := range paths {
		m[p] = corev1.FilePolicy_FILE_POLICY_UNSPECIFIED
	}
	return m
}

func TestFeatureModelFromSpec(t *testing.T) {
	if featureModelFromSpec(nil) != nil {
		t.Fatal("nil spec must map to nil model")
	}
	if featureModelFromSpec(&FeatureSpec{}) != nil {
		t.Fatal("empty spec must map to nil model")
	}
	spec := &FeatureSpec{Groups: []GroupSpec{{
		ID: "graphics", Title: "Графика", Selection: "single",
		Options: []OptionSpec{{
			ID: "sodium", Title: "Sodium", DefaultEnabled: true, Files: []string{"mods/sodium.jar"},
			JvmArgs:   []string{"-Dsodium.fast=true"},
			GameArgs:  []string{"--sodium"},
			Classpath: []string{"libraries/sodium/agent.jar"},
			Meta:      &MetaSpec{Badge: "Рекомендуется"},
			Groups:    []GroupSpec{{ID: "extras", Selection: "multi", Options: []OptionSpec{{ID: "extra", Files: []string{"mods/extra.jar"}}}}},
		}},
	}}}
	m := featureModelFromSpec(spec)
	if m == nil || len(m.Groups) != 1 {
		t.Fatal("model not built")
	}
	g := m.Groups[0]
	if g.Selection != corev1.SelectionType_SELECTION_TYPE_SINGLE {
		t.Fatalf("selection = %v", g.Selection)
	}
	if g.Options[0].Meta.Badge != "Рекомендуется" {
		t.Fatal("meta not mapped")
	}
	if g.Options[0].Groups[0].Selection != corev1.SelectionType_SELECTION_TYPE_MULTI {
		t.Fatal("nested selection not mapped")
	}
	if len(g.Options[0].JvmArgs) != 1 || g.Options[0].JvmArgs[0] != "-Dsodium.fast=true" {
		t.Fatalf("jvm args = %v", g.Options[0].JvmArgs)
	}
	if len(g.Options[0].GameArgs) != 1 || g.Options[0].GameArgs[0] != "--sodium" {
		t.Fatalf("game args = %v", g.Options[0].GameArgs)
	}
	if len(g.Options[0].Classpath) != 1 || g.Options[0].Classpath[0] != "libraries/sodium/agent.jar" {
		t.Fatalf("classpath = %v", g.Options[0].Classpath)
	}
}

func TestValidateFeatures(t *testing.T) {
	good := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{ID: "g", Selection: "single", Options: []OptionSpec{{ID: "a", Files: []string{"mods/a.jar"}}}}}})
	if err := validateFeatures(good, policyMap("mods/a.jar")); err != nil {
		t.Fatalf("valid model rejected: %v", err)
	}
	if err := validateFeatures(good, policyMap()); err == nil {
		t.Fatal("missing file must error")
	}
	uw := map[string]corev1.FilePolicy{"mods/a.jar": corev1.FilePolicy_FILE_POLICY_USER_WRITABLE}
	if err := validateFeatures(good, uw); err == nil {
		t.Fatal("user_writable optional file must error")
	}

	dup := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{
		{ID: "g", Selection: "single", Options: []OptionSpec{{ID: "a"}}},
		{ID: "g", Selection: "multi", Options: []OptionSpec{{ID: "b"}}},
	}})
	if err := validateFeatures(dup, policyMap()); err == nil {
		t.Fatal("duplicate group id must error")
	}

	uns := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{ID: "g", Selection: "nope", Options: []OptionSpec{{ID: "a"}}}}})
	if err := validateFeatures(uns, policyMap()); err == nil {
		t.Fatal("unspecified selection must error")
	}

	twoDefaults := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{ID: "g", Selection: "single", Options: []OptionSpec{{ID: "a", DefaultEnabled: true}, {ID: "b", DefaultEnabled: true}}}}})
	if err := validateFeatures(twoDefaults, policyMap()); err == nil {
		t.Fatal("more than one default in a single group must error")
	}
}

func TestValidateConstraintReferences(t *testing.T) {
	good := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{
		ID: "g", Selection: "multi",
		Options: []OptionSpec{
			{ID: "a"},
			{ID: "b", Meta: &MetaSpec{Requires: []string{"g#a"}, IncompatibleWith: []string{"g#a"}}},
		},
	}}})
	if err := validateFeatures(good, policyMap()); err != nil {
		t.Fatalf("valid constraint refs rejected: %v", err)
	}

	dangling := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{
		ID: "g", Selection: "multi",
		Options: []OptionSpec{{ID: "a", Meta: &MetaSpec{Requires: []string{"g#ghost"}}}},
	}}})
	if err := validateFeatures(dangling, policyMap()); err == nil {
		t.Fatal("dangling constraint reference must error")
	}

	selfRef := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{
		ID: "g", Selection: "multi",
		Options: []OptionSpec{{ID: "a", Meta: &MetaSpec{IncompatibleWith: []string{"g#a"}}}},
	}}})
	if err := validateFeatures(selfRef, policyMap()); err == nil {
		t.Fatal("self reference must error")
	}

	nested := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{
		ID: "g", Selection: "multi",
		Options: []OptionSpec{
			{ID: "parent", Groups: []GroupSpec{{ID: "sub", Selection: "multi", Options: []OptionSpec{{ID: "child"}}}}},
			{ID: "other", Meta: &MetaSpec{Requires: []string{"g#parent/sub#child"}}},
		},
	}}})
	if err := validateFeatures(nested, policyMap()); err != nil {
		t.Fatalf("nested address reference rejected: %v", err)
	}
}

func TestComputeAddedSizes(t *testing.T) {
	m := featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{ID: "g", Selection: "multi", Options: []OptionSpec{{ID: "a", Files: []string{"x", "y"}}}}}})
	computeAddedSizes(m.Groups, map[string]uint64{"x": 100, "y": 50})
	if got := m.Groups[0].Options[0].Meta.AddedSize; got != 150 {
		t.Fatalf("added size = %d, want 150", got)
	}
}

func TestValidateLaunchArgs(t *testing.T) {
	policy := policyMap("mods/a.jar")
	build := func(o OptionSpec) *corev1.FeatureModel {
		o.ID = "a"
		o.Files = []string{"mods/a.jar"}
		return featureModelFromSpec(&FeatureSpec{Groups: []GroupSpec{{ID: "g", Selection: "multi", Options: []OptionSpec{o}}}})
	}

	good := build(OptionSpec{
		JvmArgs:   []string{"-Xmx4G", "-javaagent:${library_directory}/a/agent.jar"},
		GameArgs:  []string{"--guard"},
		Classpath: []string{"mods/a.jar"},
	})
	if err := validateFeatures(good, policy); err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}

	bad := map[string]OptionSpec{
		"classpath override":  {JvmArgs: []string{"-cp", "/etc/passwd"}},
		"class path property": {JvmArgs: []string{"-Djava.class.path=/etc/passwd"}},
		"game dir override":   {GameArgs: []string{"--gameDir", "/tmp"}},
		"absolute entry":      {Classpath: []string{"/etc/passwd"}},
		"escaping entry":      {Classpath: []string{"../../etc/passwd"}},
		"separator entry":     {Classpath: []string{"a.jar:/etc/passwd"}},
		"empty entry":         {Classpath: []string{""}},
		"missing entry":       {Classpath: []string{"libraries/absent.jar"}},
		"lost dash":           {JvmArgs: []string{"Xmx2G"}},
		"library path":        {JvmArgs: []string{"-Djava.library.path=/tmp"}},
		"prefetched textures": {JvmArgs: []string{"-Dauthlibinjector.yggdrasil.prefetched=x"}},
		"user type":           {GameArgs: []string{"--userType", "legacy"}},
	}
	for name, spec := range bad {
		if err := validateFeatures(build(spec), policy); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
}

package module

import (
	"encoding/json"
	"testing"

	modulev1 "github.com/laminara/laminara/sdk/go/gen/laminara/module/v1"
)

type describedModule struct{ testModule }

func (describedModule) Commands() []Command { panic("commands require configuration") }

func (describedModule) Settings() SettingsDescriptor {
	return SettingsDescriptor{Module: []SettingField{{Key: "enabled", Kind: SettingBool, Default: "true", Group: "Работа"}}}
}

func TestSettingsDiscoveryDoesNotInitializeCommands(t *testing.T) {
	srv := &grpcServer{impl: describedModule{}}
	response, err := srv.DescribeSettings(t.Context(), &modulev1.DescribeSettingsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var descriptor SettingsDescriptor
	if err := json.Unmarshal(response.Description.SettingsSchemaJson, &descriptor); err != nil {
		t.Fatal(err)
	}
	if descriptor.Version != 1 || len(descriptor.Module) != 1 || descriptor.Module[0].Group != "Работа" || len(response.Description.Commands) != 0 {
		t.Fatalf("descriptor: %+v", descriptor)
	}
}

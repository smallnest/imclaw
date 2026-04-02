package permission

import (
	"reflect"
	"testing"
)

func TestResolvePresetAndDenyTools(t *testing.T) {
	resolved, err := Resolve(Policy{
		PresetName:  PresetFullAuto,
		DeniedTools: "Write, Bash",
		AuthPolicy:  "fail",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Permissions != "approve-all" {
		t.Fatalf("Permissions = %q, want approve-all", resolved.Permissions)
	}
	if resolved.AuthPolicy != "fail" {
		t.Fatalf("AuthPolicy = %q, want fail", resolved.AuthPolicy)
	}
	if contains(resolved.AllowedTools, "Write") || contains(resolved.AllowedTools, "Bash") {
		t.Fatalf("Denied tools still present: %#v", resolved.AllowedTools)
	}
	if !contains(resolved.AllowedTools, "Read") {
		t.Fatalf("Expected Read to remain allowed: %#v", resolved.AllowedTools)
	}
}

func TestResolveExplicitAllowOverridesPreset(t *testing.T) {
	resolved, err := Resolve(Policy{
		PresetName:   PresetSafeReadonly,
		AllowedTools: "Read,Grep",
		DeniedTools:  "Grep",
		Permissions:  "approve-reads",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Permissions != "approve-reads" {
		t.Fatalf("Permissions = %q, want approve-reads", resolved.Permissions)
	}
	if got, want := resolved.AllowedTools, []string{"Read"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedTools = %#v, want %#v", got, want)
	}
	if got, want := resolved.DeniedTools, []string{"Grep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeniedTools = %#v, want %#v", got, want)
	}
}

func TestResolveRejectsUnknownPreset(t *testing.T) {
	if _, err := Resolve(Policy{PresetName: "nope"}); err == nil {
		t.Fatal("expected error for unknown preset")
	}
}

func TestResolveRejectsUnknownTool(t *testing.T) {
	if _, err := Resolve(Policy{AllowedTools: "Read,Nope"}); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func contains(tools []string, target string) bool {
	for _, tool := range tools {
		if tool == target {
			return true
		}
	}
	return false
}

package permission

import (
	"reflect"
	"strings"
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
	_, err := Resolve(Policy{AllowedTools: "Read,Nope"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	// Verify error message includes list of valid tools
	errMsg := err.Error()
	if !strings.Contains(errMsg, "valid tools:") {
		t.Errorf("Error message should list valid tools, got: %s", errMsg)
	}
	// Check that error includes some known tools
	if !strings.Contains(errMsg, "Bash") || !strings.Contains(errMsg, "Read") {
		t.Errorf("Error message should include known tool names, got: %s", errMsg)
	}
}

func TestAllowedToolsCSV(t *testing.T) {
	tests := []struct {
		name     string
		policy   *ResolvedPolicy
		expected string
	}{
		{
			name:     "empty tools",
			policy:   &ResolvedPolicy{AllowedTools: []string{}},
			expected: "",
		},
		{
			name:     "single tool",
			policy:   &ResolvedPolicy{AllowedTools: []string{"Read"}},
			expected: "Read",
		},
		{
			name:     "multiple tools",
			policy:   &ResolvedPolicy{AllowedTools: []string{"Read", "Write", "Grep"}},
			expected: "Read,Write,Grep",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.AllowedToolsCSV(); got != tt.expected {
				t.Errorf("AllowedToolsCSV() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	tests := []struct {
		name     string
		policy   *ResolvedPolicy
		contains []string
	}{
		{
			name: "basic policy",
			policy: &ResolvedPolicy{
				Permissions: "approve-all",
			},
			contains: []string{"permissions=approve-all"},
		},
		{
			name: "policy with preset",
			policy: &ResolvedPolicy{
				Permissions: "approve-all",
				PresetName:  "full-auto",
			},
			contains: []string{"permissions=approve-all", "preset=full-auto"},
		},
		{
			name: "policy with allowed tools",
			policy: &ResolvedPolicy{
				Permissions:  "deny-all",
				AllowedTools: []string{"Read", "Grep"},
			},
			contains: []string{"permissions=deny-all", "allowed=Read,Grep"},
		},
		{
			name: "policy with denied tools",
			policy: &ResolvedPolicy{
				Permissions:  "approve-all",
				AllowedTools: []string{"Read", "Write"},
				DeniedTools:  []string{"Write"},
			},
			contains: []string{"permissions=approve-all", "allowed=Read", "denied=Write"},
		},
		{
			name: "policy with all fields",
			policy: &ResolvedPolicy{
				Permissions:         "approve-reads",
				PresetName:          "dev-default",
				AllowedTools:        []string{"Bash", "Read", "Write"},
				DeniedTools:         []string{},
				AuthPolicy:          "skip",
				NonInteractivePerms: "deny",
			},
			contains: []string{
				"permissions=approve-reads",
				"preset=dev-default",
				"allowed=Bash,Read,Write",
				"auth_policy=skip",
				"non_interactive_permissions=deny",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := tt.policy.Summary()
			for _, expected := range tt.contains {
				if !strings.Contains(summary, expected) {
					t.Errorf("Summary() = %q, should contain %q", summary, expected)
				}
			}
		})
	}
}

func TestSortedTools(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: nil, // SortedTools returns nil for empty input
		},
		{
			name:     "already sorted",
			input:    []string{"Bash", "Read", "Write"},
			expected: []string{"Bash", "Read", "Write"},
		},
		{
			name:     "reverse sorted",
			input:    []string{"Write", "Read", "Bash"},
			expected: []string{"Bash", "Read", "Write"},
		},
		{
			name:     "unsorted",
			input:    []string{"Grep", "Bash", "Write", "Read"},
			expected: []string{"Bash", "Grep", "Read", "Write"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SortedTools(tt.input); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("SortedTools() = %#v, want %#v", got, tt.expected)
			}
		})
	}
}

func TestResolveEmptyPreset(t *testing.T) {
	resolved, err := Resolve(Policy{PresetName: ""})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Permissions != "approve-reads" {
		t.Errorf("Default permissions = %q, want approve-reads", resolved.Permissions)
	}
	expectedTools := []string{"Bash", "Read", "Write"}
	if !reflect.DeepEqual(resolved.AllowedTools, expectedTools) {
		t.Errorf("Default allowed tools = %#v, want %#v", resolved.AllowedTools, expectedTools)
	}
}

func TestResolveWithDuplicateTools(t *testing.T) {
	resolved, err := Resolve(Policy{
		PresetName:   PresetFullAuto,
		AllowedTools: "Read,Read,Write,Read,Grep,Read",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	// Check that Read appears only once in the allowed list
	readCount := 0
	for _, tool := range resolved.AllowedTools {
		if tool == "Read" {
			readCount++
		}
	}
	if readCount != 1 {
		t.Errorf("Read appears %d times, should be deduplicated to 1", readCount)
	}
}

func TestResolveWithWhitespaceInTools(t *testing.T) {
	resolved, err := Resolve(Policy{
		PresetName:   PresetSafeReadonly,
		AllowedTools: " Read , Grep , Write ",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	expectedTools := []string{"Grep", "Read", "Write"} // Should be sorted and deduplicated
	if !reflect.DeepEqual(resolved.AllowedTools, expectedTools) {
		t.Errorf("AllowedTools with whitespace = %#v, want %#v", resolved.AllowedTools, expectedTools)
	}
}

func TestResolveDenyAllAllowedTools(t *testing.T) {
	resolved, err := Resolve(Policy{
		PresetName:  PresetFullAuto,
		DeniedTools: "Read,Write,Grep,Bash,Edit,Glob,LS,MultiEdit,NotebookEdit,TodoWrite,WebFetch,WebSearch",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved.AllowedTools) != 0 {
		t.Errorf("After denying all tools, AllowedTools = %#v, want empty slice", resolved.AllowedTools)
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

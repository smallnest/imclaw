package permission

import (
	"fmt"
	"sort"
	"strings"
)

const (
	PresetSafeReadonly = "safe-readonly"
	PresetDevDefault   = "dev-default"
	PresetFullAuto     = "full-auto"
)

var knownTools = []string{
	"Bash",
	"Edit",
	"Glob",
	"Grep",
	"LS",
	"MultiEdit",
	"NotebookEdit",
	"Read",
	"TodoWrite",
	"WebFetch",
	"WebSearch",
	"Write",
}

type Preset struct {
	Name                string
	Permissions         string
	AllowedTools        []string
	AuthPolicy          string
	NonInteractivePerms string
}

type Policy struct {
	PresetName          string
	Permissions         string
	AllowedTools        string
	DeniedTools         string
	AuthPolicy          string
	NonInteractivePerms string
}

type ResolvedPolicy struct {
	PresetName          string
	Permissions         string
	AllowedTools        []string
	DeniedTools         []string
	AuthPolicy          string
	NonInteractivePerms string
}

func Presets() []string {
	return []string{PresetSafeReadonly, PresetDevDefault, PresetFullAuto}
}

func KnownTools() []string {
	return append([]string(nil), knownTools...)
}

func Resolve(policy Policy) (*ResolvedPolicy, error) {
	base, err := presetByName(policy.PresetName)
	if err != nil {
		return nil, err
	}

	resolved := &ResolvedPolicy{
		PresetName:          base.Name,
		Permissions:         base.Permissions,
		AllowedTools:        append([]string(nil), base.AllowedTools...),
		AuthPolicy:          base.AuthPolicy,
		NonInteractivePerms: base.NonInteractivePerms,
	}

	if policy.Permissions != "" {
		resolved.Permissions = policy.Permissions
	}
	if policy.AuthPolicy != "" {
		resolved.AuthPolicy = policy.AuthPolicy
	}
	if policy.NonInteractivePerms != "" {
		resolved.NonInteractivePerms = policy.NonInteractivePerms
	}

	if policy.AllowedTools != "" {
		tools, parseErr := parseTools(policy.AllowedTools)
		if parseErr != nil {
			return nil, parseErr
		}
		resolved.AllowedTools = tools
	}

	if policy.DeniedTools != "" {
		tools, parseErr := parseTools(policy.DeniedTools)
		if parseErr != nil {
			return nil, parseErr
		}
		resolved.DeniedTools = tools
		resolved.AllowedTools = subtractTools(resolved.AllowedTools, tools)
	}

	return resolved, nil
}

func (p *ResolvedPolicy) AllowedToolsCSV() string {
	return strings.Join(p.AllowedTools, ",")
}

func (p *ResolvedPolicy) Summary() string {
	parts := []string{fmt.Sprintf("permissions=%s", p.Permissions)}
	if p.PresetName != "" {
		parts = append(parts, fmt.Sprintf("preset=%s", p.PresetName))
	}
	if len(p.AllowedTools) > 0 {
		parts = append(parts, fmt.Sprintf("allowed=%s", strings.Join(p.AllowedTools, ",")))
	}
	if len(p.DeniedTools) > 0 {
		parts = append(parts, fmt.Sprintf("denied=%s", strings.Join(p.DeniedTools, ",")))
	}
	if p.AuthPolicy != "" {
		parts = append(parts, fmt.Sprintf("auth_policy=%s", p.AuthPolicy))
	}
	if p.NonInteractivePerms != "" {
		parts = append(parts, fmt.Sprintf("non_interactive_permissions=%s", p.NonInteractivePerms))
	}
	return strings.Join(parts, " ")
}

func presetByName(name string) (Preset, error) {
	if name == "" {
		return Preset{
			Name:         "",
			Permissions:  "approve-reads",
			AllowedTools: []string{"Bash", "Read", "Write"},
		}, nil
	}

	switch name {
	case PresetSafeReadonly:
		return Preset{
			Name:         name,
			Permissions:  "deny-all",
			AllowedTools: []string{"Glob", "Grep", "LS", "Read"},
		}, nil
	case PresetDevDefault:
		return Preset{
			Name:         name,
			Permissions:  "approve-reads",
			AllowedTools: []string{"Bash", "Read", "Write"},
		}, nil
	case PresetFullAuto:
		return Preset{
			Name:         name,
			Permissions:  "approve-all",
			AllowedTools: KnownTools(),
		}, nil
	default:
		return Preset{}, fmt.Errorf("unknown permission preset %q (valid: %s)", name, strings.Join(Presets(), ", "))
	}
}

func parseTools(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	set := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		tool := strings.TrimSpace(part)
		if tool == "" {
			continue
		}
		if !isKnownTool(tool) {
			return nil, fmt.Errorf("unknown tool %q in permission policy", tool)
		}
		set[tool] = struct{}{}
	}

	tools := make([]string, 0, len(set))
	for _, tool := range knownTools {
		if _, ok := set[tool]; ok {
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

func isKnownTool(tool string) bool {
	for _, known := range knownTools {
		if tool == known {
			return true
		}
	}
	return false
}

func subtractTools(allowed, denied []string) []string {
	if len(denied) == 0 {
		return append([]string(nil), allowed...)
	}
	denySet := make(map[string]struct{}, len(denied))
	for _, tool := range denied {
		denySet[tool] = struct{}{}
	}
	filtered := make([]string, 0, len(allowed))
	for _, tool := range allowed {
		if _, denied := denySet[tool]; denied {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func SortedTools(tools []string) []string {
	out := append([]string(nil), tools...)
	sort.Strings(out)
	return out
}

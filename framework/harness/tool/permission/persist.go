package permission

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// PresetFile is the on-disk shape of a profile-level permission
// preset (referenced by Profile.PermissionsPreset).
//
// JSON, not TOML, so we don't pull in the TOML writer alongside the
// reader we already ship. The preset path in the profile remains the
// caller's responsibility.
type PresetFile struct {
	SchemaVersion int    `json:"schema_version"`
	Rules         []Rule `json:"rules"`
}

// LoadPreset reads a permission preset from disk. Returns an empty
// PresetFile if the file doesn't exist.
func LoadPreset(path string) (*PresetFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &PresetFile{SchemaVersion: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var pf PresetFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("permission: parse %s: %w", path, err)
	}
	if pf.SchemaVersion == 0 {
		pf.SchemaVersion = 1
	}
	return &pf, nil
}

// SavePreset writes a preset to disk atomically (.tmp + rename).
// Rules are sorted by (Tool, ArgvGlob) for stable diffs.
func SavePreset(path string, pf *PresetFile) error {
	if pf.SchemaVersion == 0 {
		pf.SchemaVersion = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	sort.Slice(pf.Rules, func(i, j int) bool {
		a, b := pf.Rules[i], pf.Rules[j]
		if a.Tool != b.Tool {
			return a.Tool < b.Tool
		}
		return a.ArgvGlob < b.ArgvGlob
	})
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Promote moves a session-scoped rule to the profile-level preset.
// Returns the path of the preset file that was updated, plus the
// rule that landed (de-duplicated against existing rules).
//
// `sessionRules` is the engine's current session-scoped rule set;
// `index` selects which one to promote.
func (e *Engine) Promote(presetPath string, session ids.SessionID, index int) (Rule, error) {
	rules := e.ListSessionRules(session)
	if index < 0 || index >= len(rules) {
		return Rule{}, fmt.Errorf("permission: index %d out of range (have %d session rules)", index, len(rules))
	}
	rule := rules[index]
	pf, err := LoadPreset(presetPath)
	if err != nil {
		return Rule{}, err
	}
	// De-duplicate against existing rules.
	for _, existing := range pf.Rules {
		if existing.Tool == rule.Tool && existing.ArgvGlob == rule.ArgvGlob {
			return rule, nil // already present
		}
	}
	pf.Rules = append(pf.Rules, rule)
	if err := SavePreset(presetPath, pf); err != nil {
		return Rule{}, err
	}
	// Reflect the promotion in the in-memory engine as well.
	e.mu.Lock()
	e.profileRules = append(e.profileRules, rule)
	// Remove from session scope.
	src := e.sessionRules[session]
	e.sessionRules[session] = append(src[:index], src[index+1:]...)
	e.mu.Unlock()
	return rule, nil
}

// PromoteByDescription is a string-based convenience for callers that
// want to identify the rule by `Tool:ArgvGlob` rather than by index.
func (e *Engine) PromoteByDescription(presetPath string, session ids.SessionID, desc string) (Rule, error) {
	parts := strings.SplitN(desc, ":", 2)
	if len(parts) == 0 || parts[0] == "" {
		return Rule{}, errors.New("permission: PromoteByDescription wants 'Tool[:ArgvGlob]'")
	}
	tool := parts[0]
	argv := ""
	if len(parts) > 1 {
		argv = parts[1]
	}
	rules := e.ListSessionRules(session)
	for i, r := range rules {
		if r.Tool == tool && r.ArgvGlob == argv {
			return e.Promote(presetPath, session, i)
		}
	}
	return Rule{}, fmt.Errorf("permission: no matching session rule for %q", desc)
}

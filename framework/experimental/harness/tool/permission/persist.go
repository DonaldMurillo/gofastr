package permission

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// PresetFile is the JSON form of a profile's permission preset.
type PresetFile struct {
	SchemaVersion int    `json:"schema_version"`
	Rules         []Rule `json:"rules"`
}

// LoadPreset reads the preset at path, or returns an empty one when the file does not exist.
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

// SavePreset writes pf to path owner-only through a temporary file and a rename, its rules sorted by Tool and ArgvGlob.
func SavePreset(path string, pf *PresetFile) error {
	if pf.SchemaVersion == 0 {
		pf.SchemaVersion = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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
	if err := fileperm.WriteOwnerOnly(tmp, data); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Promote moves session's rule at index into the preset at presetPath and the engine's profile rules, replacing the action of a rule with the same Tool, ArgvGlob and Glob.
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
	if upsertRule(&pf.Rules, rule) {
		if err := SavePreset(presetPath, pf); err != nil {
			return Rule{}, err
		}
	}
	e.mu.Lock()
	upsertRule(&e.profileRules, rule)
	if src := e.sessionRules[session]; len(src) > 0 {
		if i := slices.Index(src, rule); i >= 0 {
			e.sessionRules[session] = slices.Delete(src, i, i+1)
		}
	}
	e.mu.Unlock()
	return rule, nil
}

func upsertRule(rules *[]Rule, r Rule) bool {
	for i, have := range *rules {
		if have.Tool == r.Tool && have.ArgvGlob == r.ArgvGlob && have.Glob == r.Glob {
			if have.Action == r.Action {
				return false
			}
			(*rules)[i].Action = r.Action
			return true
		}
	}
	*rules = append(*rules, r)
	return true
}

// PromoteByDescription promotes the session rule named "Tool" or "Tool:ArgvGlob".
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

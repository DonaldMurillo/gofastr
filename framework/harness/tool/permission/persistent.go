package permission

// Persistent permission rules — saved to disk so the user only has
// to grant "Allow always" once. Loaded on Engine boot, written
// atomically (write to tmp + rename) on every AddPersistentRule.
//
// File format: JSON wrapper { "version": 1, "rules": [...] }.
// Version field lets us evolve the schema without breaking old files.
// The on-disk path is set per-Engine via PersistencePath — the
// harness composition layer points it at
// $XDG_CONFIG_HOME/gofastr/harness/permissions.json.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
)

// persistentFile is the on-disk shape. Keep it forward-compatible:
// add fields with json tags + omitempty, never rename.
type persistentFile struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

const persistentSchemaVersion = 1

// LoadPersistentRules reads the on-disk file into the engine's
// persistent rule list. Missing file is NOT an error — first run.
func (e *Engine) LoadPersistentRules() error {
	if e.PersistencePath == "" {
		return nil
	}
	data, err := os.ReadFile(e.PersistencePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", e.PersistencePath, err)
	}
	var f persistentFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", e.PersistencePath, err)
	}
	e.mu.Lock()
	e.persistentRules = append([]Rule{}, f.Rules...)
	e.mu.Unlock()
	return nil
}

// AddPersistentRule appends a rule to the persistent list AND writes
// the file atomically. Returns error if the disk write fails — the
// in-memory list is still updated either way (best-effort durability).
func (e *Engine) AddPersistentRule(r Rule) error {
	e.mu.Lock()
	e.persistentRules = append(e.persistentRules, r)
	rules := append([]Rule{}, e.persistentRules...) // snapshot for write
	e.mu.Unlock()
	if e.PersistencePath == "" {
		return nil // in-memory only
	}
	return savePersistent(e.PersistencePath, rules)
}

// ListPersistentRules returns a copy of the current persistent rules.
func (e *Engine) ListPersistentRules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Rule, len(e.persistentRules))
	copy(out, e.persistentRules)
	return out
}

// savePersistent writes the rules to path atomically (write tmp +
// rename). Creates the parent directory if needed.
func savePersistent(path string, rules []Rule) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(persistentFile{
		Version: persistentSchemaVersion,
		Rules:   rules,
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}
	return nil
}

// AnswerToRuleWithPersist extends AnswerToRule with a "should persist"
// flag. True iff Scope == ScopeAlways, meaning callers should call
// AddPersistentRule to write it to disk.
//
// Returns (rule, shouldPersist, ok). ok=false for unrecognized scopes
// (mirrors AnswerToRule for compatibility).
func AnswerToRuleWithPersist(toolName, argvSummary string, ans control.AnswerPermission) (Rule, bool, bool) {
	if ans.Scope == control.ScopeAlways {
		act := DecisionAsk
		switch ans.Decision {
		case control.DecisionAllow:
			act = DecisionAllow
		case control.DecisionDeny:
			act = DecisionDeny
		}
		// Persist as a tool-scoped rule with argv glob if available.
		return Rule{Tool: toolName, ArgvGlob: argvSummary, Action: act}, true, true
	}
	r, ok := AnswerToRule(toolName, argvSummary, ans)
	return r, false, ok
}

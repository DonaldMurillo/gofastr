package permission

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

type persistentFile struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

const persistentSchemaVersion = 1

// LoadPersistentRules replaces the persistent rules with those in PersistencePath; a missing file is no error.
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

// AddPersistentRule adds r, stamped with WorkingDir when it names no Dir, and saves the list; a failed save still keeps r in memory.
func (e *Engine) AddPersistentRule(r Rule) error {
	e.mu.Lock()
	if r.Dir == "" {
		r.Dir = e.WorkingDir
	}
	e.persistentRules = append(e.persistentRules, r)
	rules := append([]Rule{}, e.persistentRules...)
	e.mu.Unlock()
	if e.PersistencePath == "" {
		return nil
	}
	return savePersistent(e.PersistencePath, rules)
}

// ListPersistentRules returns a copy of the persistent rules.
func (e *Engine) ListPersistentRules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Rule, len(e.persistentRules))
	copy(out, e.persistentRules)
	return out
}

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
	if err := fileperm.WriteOwnerOnly(tmp, data); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}
	return nil
}

// AnswerToRuleWithPersist is AnswerToRule plus ScopeAlways, which returns a literal rule and true for persist.
func AnswerToRuleWithPersist(toolName, argvSummary string, ans control.AnswerPermission) (Rule, bool, bool) {
	if ans.Scope == control.ScopeAlways {
		act := DecisionAsk
		switch ans.Decision {
		case control.DecisionAllow:
			act = DecisionAllow
		case control.DecisionDeny:
			act = DecisionDeny
		}
		return Rule{Tool: toolName, ArgvGlob: argvSummary, Action: act}, true, true
	}
	r, ok := AnswerToRule(toolName, argvSummary, ans)
	return r, false, ok
}

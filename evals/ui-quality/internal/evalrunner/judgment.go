package evalrunner

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

const maxJudgeAttempts = 2

func validateJudgment(j Judgment, wantCandidateID string) error {
	if j.CandidateID != wantCandidateID {
		return fmt.Errorf("echoed candidate %q, want %q", j.CandidateID, wantCandidateID)
	}
	for name, value := range map[string]float64{
		"hierarchy": j.Dimensions.Hierarchy, "composition": j.Dimensions.Composition,
		"typography": j.Dimensions.Typography, "product_specificity": j.Dimensions.ProductSpecificity,
		"density": j.Dimensions.Density, "component_polish": j.Dimensions.ComponentPolish,
		"responsive_intent": j.Dimensions.ResponsiveIntent, "theme_coherence": j.Dimensions.ThemeCoherence,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 10 {
			return fmt.Errorf("dimension %s is outside 0..10: %v", name, value)
		}
	}
	if err := validateFeedbackList("strongest_visible_decisions", j.StrongestVisible); err != nil {
		return err
	}
	if err := validateFeedbackList("weakest_visible_decisions", j.WeakestVisible); err != nil {
		return err
	}
	if err := validateJudgeText("shadcn_level_rationale", j.ShadcnLevelRationale, 20, 1000); err != nil {
		return err
	}
	return validateJudgeText("next_iteration", j.NextIteration, 20, 700)
}

func validateFeedbackList(name string, items []string) error {
	if len(items) < 1 || len(items) > 4 {
		return fmt.Errorf("%s has %d items, want 1..4", name, len(items))
	}
	for i, item := range items {
		if err := validateJudgeText(fmt.Sprintf("%s[%d]", name, i), item, 20, 1200); err != nil {
			return err
		}
	}
	return nil
}

func validateJudgeText(name, value string, minRunes, maxRunes int) error {
	text := strings.TrimSpace(value)
	length := utf8.RuneCountInString(text)
	if length < minRunes || length > maxRunes {
		return fmt.Errorf("%s has %d characters, want %d..%d", name, length, minRunes, maxRunes)
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"<|channel|>", "codex_output_schema", "response format", "schema parser",
		"schema-conforming", "only one final", "i need send", "i'll send",
		"current analysis", "accidentally", "weakest_visible_decisions\"",
	} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("%s contains judge-process chatter marker %q", name, marker)
		}
	}
	return nil
}

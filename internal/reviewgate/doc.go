// Package reviewgate holds the tests for the review-gate skill's runner,
// .claude/skills/review-gate/review3.py, which the pre-commit hook calls.
// The hook fails a commit on exit 1 alone, so these tests pin which
// answers produce it.
package reviewgate

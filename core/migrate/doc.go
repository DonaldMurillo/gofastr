// Package migrate runs versioned SQL migrations, each in a default or named
// group, under a cross-process lock, and records them in a tracking table.
// The file format and the group rules are in docs/content/migrations.md.
package migrate

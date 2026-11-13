package main

import "testing"

func TestVersionCheckApplies(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		args []string
		want bool
	}{
		{"dev", nil, true},
		{"build", []string{"--pkg", "./cmd/app"}, true},
		{"dev", []string{"--help"}, false},
		{"migrate", []string{"up", "-h"}, false},
		{"dve", nil, false},
		{"init", []string{"app"}, false},
		{"upgrade", nil, false},
		{"version", nil, false},
	} {
		if got := versionCheckApplies(tc.cmd, tc.args); got != tc.want {
			t.Errorf("versionCheckApplies(%q, %q) = %v; want %v", tc.cmd, tc.args, got, tc.want)
		}
	}
}

func TestProjectDirFlag(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		args []string
		want string
	}{
		{"dev", []string{"--dir=../app"}, "../app"},
		{"dev", []string{"--addr", ":9000", "--dir", "/srv/app"}, "/srv/app"},
		{"dev", nil, ""},
		{"build", []string{"--dir=../app"}, ""},
		{"dev", []string{"--dir=./default", "--dir=/srv/app"}, "/srv/app"},
		{"dev", []string{"--dir", "./default", "--dir=/srv/app"}, "/srv/app"},
	} {
		if got := projectDirFlag(tc.cmd, tc.args); got != tc.want {
			t.Errorf("projectDirFlag(%q, %q) = %q; want %q", tc.cmd, tc.args, got, tc.want)
		}
	}
}

func TestVersionCheckCoversEveryCommand(t *testing.T) {
	skipped := map[string]bool{"init": true, "version": true, "docs": true, "doc": true, "upgrade": true, "help": true}
	seen := 0
	for _, command := range commands {
		for _, name := range command.names {
			seen++
			if command.versionChecked == skipped[name] {
				t.Errorf("command %q is both checked and skipped, or neither", name)
			}
		}
	}
	if seen < 20 {
		t.Fatalf("found only %d command names", seen)
	}
}

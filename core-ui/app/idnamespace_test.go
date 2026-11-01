package app

import "testing"

func TestIDNamespaceIsOneToOne(t *testing.T) {
	addrs := []string{
		"l:shell#aside", "l:shell#aside-a", "l:shell#aside_a", "l:shell#Aside",
		"l:a-b#c", "l:a#b-c", "g:/g:l:a#b", "l:shell#aside-",
	}
	seen := map[string]string{}
	for _, a := range addrs {
		ns := idNamespace(a)
		if prev, ok := seen[ns]; ok {
			t.Errorf("%q and %q share namespace %q", prev, a, ns)
		}
		seen[ns] = a
	}
	if got := idNamespace("l:shell#aside"); got != "l-shell-aside" {
		t.Errorf("l:shell#aside = %q, want l-shell-aside", got)
	}
}

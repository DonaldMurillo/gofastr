package tui

import "strings"

func searchScrollback(lines []string, query string) []int {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var hits []int
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), q) {
			hits = append(hits, i)
		}
	}
	return hits
}

func (t *TUI) activateSearch() {
	t.searchActive = true
	t.searchQuery = ""
	t.searchHits = nil
}

func (t *TUI) deactivateSearch() {
	t.searchActive = false
	t.searchQuery = ""
	t.searchHits = nil
}

func (t *TUI) updateSearch() {
	t.searchHits = searchScrollback(t.scrollback, t.searchQuery)
}

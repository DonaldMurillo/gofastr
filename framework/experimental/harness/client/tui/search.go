package tui

// Scrollback search (Ctrl-R). When active, typing builds a query
// that filters which scrollback lines are visible; Esc cancels;
// Enter accepts and stays at the current match.
//
// Design: no regex (keeps the predictable, fast path); case-
// insensitive substring match. Each typed key updates searchQuery
// and re-runs searchScrollback.

import "strings"

// searchScrollback returns the indices of every line in lines that
// contains query (case-insensitive). Empty query → no hits (instead
// of matching every line — saves the user from accidentally hiding
// everything).
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

// activateSearch enters search mode. Subsequent keys go to the
// search buffer until Esc or Enter.
//
// Caller must hold t.mu.
func (t *TUI) activateSearch() {
	t.searchActive = true
	t.searchQuery = ""
	t.searchHits = nil
}

// deactivateSearch leaves search mode and clears the query.
//
// Caller must hold t.mu.
func (t *TUI) deactivateSearch() {
	t.searchActive = false
	t.searchQuery = ""
	t.searchHits = nil
}

// updateSearch recomputes hits against the current scrollback.
//
// Caller must hold t.mu.
func (t *TUI) updateSearch() {
	t.searchHits = searchScrollback(t.scrollback, t.searchQuery)
}

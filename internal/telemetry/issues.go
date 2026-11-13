package telemetry

import (
	"encoding/binary"
	"encoding/hex"
	"hash/fnv"
	"slices"
	"time"
)

// Issue is one way requests are failing: every failed request with the
// same route, status and error, once the parts of the error that change
// from one failure to the next (numbers, IDs) are set aside.
type Issue struct {
	// ID names the issue in a URL and in TraceQuery.Issue.
	ID     string
	Route  string
	Status int
	// Err is the error with its changing parts replaced; Example is
	// the latest failure's error as it was written.
	Err     string
	Example string
	Count   int
	First   time.Time
	Last    time.Time
	// LastTraceID is the latest failure, to open as an example.
	LastTraceID string
}

// maxIssues bounds how many distinct issues one listing tracks. Past
// it, failures that fit no issue already seen are counted as left out:
// an app failing in two thousand different ways has a bug in how its
// errors are worded, and the list cannot help.
const maxIssues = 2000

type issueKey struct {
	route  string
	status int
	err    string
}

// stableErr replaces the parts of an error that differ between two
// failures of the same kind: a run of digits becomes N, and a run of
// eight or more hex digits and dashes holding a digit (an ID, an
// address, a UUID) becomes ID. "dial tcp 10.0.0.7:5432" and "dial tcp
// 10.0.0.9:5432" then group as one.
func stableErr(s string) string {
	isHex := func(c byte) bool {
		return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		j, digits := i, 0
		for j < len(s) && (isHex(s[j]) || (s[j] == '-' && j > i)) {
			if s[j] >= '0' && s[j] <= '9' {
				digits++
			}
			j++
		}
		switch {
		case j-i >= 8 && digits > 0:
			out = append(out, "ID"...)
			i = j
		case s[i] >= '0' && s[i] <= '9':
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			out = append(out, 'N')
		default:
			// Not an ID: keep this byte and look again from the next,
			// so a word made of hex letters ("deed", "face") stays.
			out = append(out, s[i])
			i++
		}
	}
	return string(out)
}

func (k issueKey) id() string {
	h := fnv.New64a()
	var n [8]byte
	for _, part := range []string{k.route, k.err} {
		binary.LittleEndian.PutUint64(n[:], uint64(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	binary.LittleEndian.PutUint64(n[:], uint64(k.status))
	h.Write(n[:])
	return hex.EncodeToString(h.Sum(nil))
}

func keyOf(sum TraceSummary) issueKey {
	return issueKey{route: sum.Route, status: sum.Status, err: stableErr(sum.Err)}
}

// IssueID returns the ID of the issue a failed trace belongs to.
func IssueID(sum TraceSummary) string { return keyOf(sum).id() }

// Issues groups the failed requests that ended in [from, to] and
// returns up to limit groups, the most recently seen first. Every
// failed request is kept, so the counts are exact. leftOut counts the
// failures past the two thousandth distinct issue.
func (s *Store) Issues(from, to time.Time, limit int) (issues []Issue, leftOut int, err error) {
	if limit <= 0 {
		return nil, 0, nil
	}
	groups := map[issueKey]*Issue{}
	err = s.scan(KindTraces, from, to, true, true, func(line []byte) bool {
		sum, ok := summary(line)
		if !ok || !sum.Error || !inWindow(sum.Time, from, to) {
			return true
		}
		k := keyOf(sum)
		g := groups[k]
		if g == nil {
			if len(groups) >= maxIssues {
				leftOut++
				return true
			}
			// Newest first: the first failure met is the latest.
			g = &Issue{ID: k.id(), Route: sum.Route, Status: sum.Status, Err: k.err, Example: sum.Err,
				First: sum.Time, Last: sum.Time, LastTraceID: sum.TraceID}
			groups[k] = g
		}
		g.Count++
		if sum.Time.Before(g.First) {
			g.First = sum.Time
		}
		if sum.Time.After(g.Last) {
			g.Last, g.LastTraceID, g.Example = sum.Time, sum.TraceID, sum.Err
		}
		return true
	})
	if err != nil {
		return nil, 0, err
	}
	issues = make([]Issue, 0, len(groups))
	for _, g := range groups {
		issues = append(issues, *g)
	}
	slices.SortFunc(issues, func(a, b Issue) int {
		if c := b.Last.Compare(a.Last); c != 0 {
			return c
		}
		// Seen in the same instant: the one that happens more comes first.
		if a.Count != b.Count {
			if a.Count > b.Count {
				return -1
			}
			return 1
		}
		return cmpString(a.ID, b.ID)
	})
	return issues[:min(len(issues), limit)], leftOut, nil
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

package telemetry

import "testing"

// A tool returns at most its cap, whatever limit asks for: an agent
// asking for a million lines gets five hundred, not the day's file.
func TestLimitArgIsCapped(t *testing.T) {
	for _, c := range []struct {
		in   any
		want int
	}{{nil, 50}, {1.0, 1}, {200.0, 200}, {201.0, 200}, {1e12, 200}, {7, 7}} {
		params := map[string]any{}
		if c.in != nil {
			params["limit"] = c.in
		}
		got, err := limitArg(params, 50, 200)
		if err != nil || got != c.want {
			t.Errorf("limit %v: got %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}

package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

// scriptedCache answers each Get from a fixed script, one entry per call,
// so a test can tell the fast-path read apart from the in-flight recheck.
type scriptedCache struct {
	gets []error
	n    int
}

func (s *scriptedCache) Get(context.Context, string, any) error {
	if s.n >= len(s.gets) {
		return errors.New("scriptedCache: unexpected Get")
	}
	err := s.gets[s.n]
	s.n++
	return err
}

func (s *scriptedCache) Set(context.Context, string, any, time.Duration) error { return nil }
func (s *scriptedCache) Delete(context.Context, string) error                  { return nil }
func (s *scriptedCache) Exists(context.Context, string) (bool, error)          { return false, nil }
func (s *scriptedCache) Clear(context.Context) error                           { return nil }

// A backend error is not a miss at either read: GetOrSet surfaces it and
// never runs the loader. Each case scripts one guard alone, so removing
// either guard fails its own case.
func TestGetOrSetBackendErrorSkipsLoader(t *testing.T) {
	outage := errors.New("connection refused")
	cases := []struct {
		name string
		gets []error
	}{
		{"fast-path read fails", []error{outage, ErrCacheMiss}},
		{"miss then recheck fails", []error{ErrCacheMiss, outage}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &scriptedCache{gets: tc.gets}
			loads := 0
			var got string
			err := GetOrSet(context.Background(), c, "k", time.Minute, &got,
				func(context.Context) (any, error) { loads++; return "loaded", nil })
			if !errors.Is(err, outage) {
				t.Fatalf("GetOrSet err = %v, want the backend error", err)
			}
			if loads != 0 {
				t.Fatalf("loader ran %d time(s) on a backend error", loads)
			}
		})
	}
}

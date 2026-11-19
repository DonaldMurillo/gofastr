package featureflag

import (
	"errors"
	"fmt"
)

func checkFlag(f Flag) (Flag, error) {
	if f.Key == "" {
		return f, errors.New("featureflag: empty key")
	}
	for i, u := range f.Users {
		if _, _, err := ParseUserEntry(u); err != nil {
			return f, fmt.Errorf("featureflag: flag %q: users[%d]: %w", f.Key, i, err)
		}
	}
	f.Rollout = min(max(f.Rollout, 0), 100)
	return f, nil
}

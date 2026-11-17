package localrun

import "fmt"

func checkOwner(dir, owner string, mine ...string) error {
	for _, m := range mine {
		if m != "" && owner == m {
			return nil
		}
	}
	return fmt.Errorf("localrun: %s is owned by %s, not this user", dir, owner)
}

package localrun

import "fmt"

// checkOwner refuses a registry directory whose owner is none of the
// identities this process creates files as. Owners are compared as
// strings (a uid, or a canonical SID), so one test covers every OS.
func checkOwner(dir, owner string, mine ...string) error {
	for _, m := range mine {
		if m != "" && owner == m {
			return nil
		}
	}
	return fmt.Errorf("localrun: %s is owned by %s, not this user", dir, owner)
}

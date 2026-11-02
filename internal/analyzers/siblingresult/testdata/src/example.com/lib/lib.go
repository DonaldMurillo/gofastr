// Package lib declares a marked function another package calls.
package lib

// Session is a minted session.
type Session struct{ Token string }

// Mint creates a session; pending means the second factor is owed.
//
//gofastr:mustuse pending
func Mint(user string) (sess *Session, pending bool, err error) { // want Mint:`mustUse\[1\]`
	return &Session{Token: user}, user == "2fa", nil
}

// Lookup is unmarked: its ok is the caller's to read or not.
func Lookup(key string) (string, bool) { return key, key != "" }

// BadMark names a result that does not exist.
//
//gofastr:mustuse missing
func BadMark() (int, error) { return 0, nil } // want `//gofastr:mustuse names "missing", which is not a result of BadMark`

// Empty names nothing.
//
//gofastr:mustuse
func Empty() (int, bool) { return 0, false } // want `//gofastr:mustuse names no result of Empty`

// Prose carries a different directive that only shares the prefix.
//
//gofastr:mustusefoo n
func Prose() (int, bool) { return 0, false }

// Both marks two results, by name and by index.
//
//gofastr:mustuse a 1
func Both() (a, b int, err error) { return 0, 0, nil } // want Both:`mustUse\[0 1\]`

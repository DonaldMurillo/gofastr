package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// A cloned repo's .harness-secrets/env can hold the repo author's own
// provider keys. When the operator has none of their own, LoadRepo set
// them silently and the session ran, and billed, on the author's
// account, with the author able to read every prompt on their provider
// dashboard. LoadWithConsent asks first: once per repository and key
// set, remembered like an "Allow always" permission scoped to the
// directory, and a run with nobody to ask refuses the keys.

// ApprovalsFile is the file, under the harness config directory, that
// remembers which repositories may supply which provider keys.
const ApprovalsFile = "repo-secrets.json"

// maxApprovalsBytes bounds the approvals read.
const maxApprovalsBytes = 1 << 20

// Request is what a consent prompt shows: the secrets file and the
// provider keys it would set because the environment lacks them.
type Request struct {
	Path string   // the .harness-secrets/env file
	Dir  string   // the repository directory holding it
	Keys []string // sorted key names, never values
}

// Ask decides a Request. It returns approve=true to use the keys (and
// remember the answer). A nil Ask means nobody can be asked.
type Ask func(Request) (approve bool, err error)

// Result says what LoadWithConsent did.
type Result struct {
	Request
	// Applied is true when the keys were set in the environment.
	Applied bool
	// Remembered is true when they were set from an earlier approval.
	Remembered bool
}

// ErrRefused is returned when the file would supply keys and neither an
// earlier approval, allow, nor ask approved them. The keys are not set.
var ErrRefused = errors.New("secrets: repository-supplied provider keys not approved")

// LoadWithConsent finds .harness-secrets/env above dir and sets the
// provider keys it would supply only with consent: a remembered
// approval for this directory covering every key, allow (an explicit
// operator flag), or ask returning true, which is then remembered in
// approvalsPath. With none of them it sets nothing and returns
// ErrRefused, so a non-interactive run never borrows a repo's account.
// A file that supplies nothing the environment lacks asks nothing.
func LoadWithConsent(dir, approvalsPath string, allow bool, ask Ask) (Result, error) {
	path, ok := findSecretsFile(dir)
	if !ok {
		return Result{}, nil
	}
	pending, err := pendingFromFile(path)
	if err != nil {
		return Result{}, err
	}
	if len(pending) == 0 {
		return Result{}, nil
	}
	repo := filepath.Dir(filepath.Dir(path))
	if abs, err := filepath.Abs(repo); err == nil {
		repo = abs
	}
	req := Request{Path: path, Dir: repo}
	for _, p := range pending {
		req.Keys = append(req.Keys, p.key)
	}
	slices.Sort(req.Keys)
	req.Keys = slices.Compact(req.Keys)
	res := Result{Request: req}

	approvals, err := readApprovals(approvalsPath)
	if err != nil {
		return res, err
	}
	switch {
	case approvals.covers(req):
		res.Remembered = true
	case allow:
	case ask != nil:
		approve, err := ask(req)
		if err != nil {
			return res, err
		}
		if !approve {
			return res, ErrRefused
		}
		if err := approvals.remember(approvalsPath, req); err != nil {
			return res, err
		}
	default:
		return res, ErrRefused
	}
	if err := apply(pending); err != nil {
		return res, err
	}
	res.Applied = true
	return res, nil
}

type approval struct {
	Dir  string   `json:"dir"`
	Keys []string `json:"keys"`
}

type approvalList struct {
	Approvals []approval `json:"approvals"`
}

func readApprovals(path string) (approvalList, error) {
	var list approvalList
	if path == "" {
		return list, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return list, nil
	}
	if err != nil {
		return list, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxApprovalsBytes))
	if err != nil {
		return list, err
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return approvalList{}, fmt.Errorf("secrets: %s: %w", path, err)
	}
	return list, nil
}

// covers reports whether an earlier approval for req.Dir named every
// key req asks about. A new key in the file asks again.
func (l approvalList) covers(req Request) bool {
	for _, a := range l.Approvals {
		if a.Dir != req.Dir {
			continue
		}
		all := true
		for _, k := range req.Keys {
			if !slices.Contains(a.Keys, k) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// remember adds req's directory and keys and writes the list owner-only.
func (l approvalList) remember(path string, req Request) error {
	if path == "" {
		return nil
	}
	kept := l.Approvals[:0:0]
	keys := slices.Clone(req.Keys)
	for _, a := range l.Approvals {
		if a.Dir == req.Dir {
			keys = append(keys, a.Keys...)
			continue
		}
		kept = append(kept, a)
	}
	slices.Sort(keys)
	l.Approvals = append(kept, approval{Dir: req.Dir, Keys: slices.Compact(keys)})
	body, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileperm.WriteOwnerOnly(path, body)
}

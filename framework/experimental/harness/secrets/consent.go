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

// ApprovalsFile is the name of the file, in the harness config directory, that remembers approved repositories and keys.
const ApprovalsFile = "repo-secrets.json"

const maxApprovalsBytes = 1 << 20

// Request is what a consent prompt shows: the secrets file, its repository and the sorted key names it would set.
type Request struct {
	Path string
	Dir  string
	Keys []string
}

// Ask decides a Request; true approves and remembers it. A nil Ask means nobody can be asked.
type Ask func(Request) (approve bool, err error)

// Result says what LoadWithConsent did: Applied when keys were set, Remembered when an earlier approval allowed it.
type Result struct {
	Request
	Applied    bool
	Remembered bool
}

// ErrRefused is returned when nothing approved the keys the file would supply; none are set.
var ErrRefused = errors.New("secrets: repository-supplied provider keys not approved")

// LoadWithConsent sets the keys .harness-secrets/env above dir would supply only when an approval in approvalsPath covers them,
// allow is true, or ask approves (and is remembered); otherwise it returns ErrRefused.
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

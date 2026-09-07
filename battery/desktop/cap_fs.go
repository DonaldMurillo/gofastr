package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// fsCapability: read/write/stat on paths the dialogs capability (or an
// in-process AllowPath call) put on the allow-list. The allow-list is
// per-process; whole-disk access is a plugin's call, not the core
// default.

// fsReadLimit caps a readText call (8 MiB).
const fsReadLimit = 8 << 20

// errPathNotAllowed is the fixed refusal for off-list paths; it never
// echoes the path back.
var errPathNotAllowed = &Error{Code: CodeDenied, Message: "path is not accessible"}

func (b *Battery) fsCapability() Capability {
	return Capability{
		Name:        "fs",
		Description: "Read and write files the user picked through a dialog in this session.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "readText",
				Description: "Reads a UTF-8 text file (at most 8 MiB) from the allow-list.",
				Permission:  "fs:read",
				Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
				Output:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					req, derr := decodeFSPath(in)
					if derr != nil {
						return nil, derr
					}
					if !b.pathAllowed(req.Path) {
						return nil, errPathNotAllowed
					}
					f, err := os.Open(req.Path)
					if err != nil {
						return nil, fsError(err)
					}
					defer f.Close()
					data, err := io.ReadAll(io.LimitReader(f, fsReadLimit+1))
					if err != nil {
						return nil, fsError(err)
					}
					if len(data) > fsReadLimit {
						return nil, &Error{Code: CodeInvalidInput, Message: "file is larger than the 8 MiB read limit"}
					}
					return fsTextOutput{Text: string(data)}, nil
				},
			},
			{
				Name:        "writeText",
				Description: "Writes a text file atomically (temp file + rename) to the allow-list; new files are 0600.",
				Permission:  "fs:write",
				Input: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"text":{"type":"string"}},` +
					`"required":["path","text"]}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					var req struct {
						Path string `json:"path"`
						Text string `json:"text"`
					}
					if err := decodeInput(in, &req); err != nil {
						return nil, err
					}
					if !b.pathAllowed(req.Path) {
						return nil, errPathNotAllowed
					}
					if err := writeFileAtomic(req.Path, []byte(req.Text)); err != nil {
						return nil, fsError(err)
					}
					return nil, nil
				},
			},
			{
				Name:        "stat",
				Description: "Returns size, modification time, and directory flag for an allow-listed path.",
				Permission:  "fs:read",
				Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
				Output: json.RawMessage(`{"type":"object","properties":{` +
					`"size":{"type":"integer"},"modTime":{"type":"string"},"isDir":{"type":"boolean"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					req, derr := decodeFSPath(in)
					if derr != nil {
						return nil, derr
					}
					if !b.pathAllowed(req.Path) {
						return nil, errPathNotAllowed
					}
					info, err := os.Stat(req.Path)
					if err != nil {
						return nil, fsError(err)
					}
					return fsStatOutput{
						Size:    info.Size(),
						ModTime: info.ModTime().UTC().Format(time.RFC3339Nano),
						IsDir:   info.IsDir(),
					}, nil
				},
			},
		},
	}
}

// fsPathInput is the {path} shape shared by readText and stat.
type fsPathInput struct {
	Path string `json:"path"`
}

// decodeFSPath strictly decodes a {path} input.
func decodeFSPath(in json.RawMessage) (fsPathInput, *Error) {
	var req fsPathInput
	if err := decodeInput(in, &req); err != nil {
		return req, err
	}
	if req.Path == "" {
		return req, &Error{Code: CodeInvalidInput, Message: "path is required"}
	}
	return req, nil
}

// fsError maps OS errors onto the closed code set without echoing the
// path: not-exist → not_found, permission → denied, everything else →
// internal.
func fsError(err error) *Error {
	if errors.Is(err, fs.ErrNotExist) {
		return &Error{Code: CodeNotFound, Message: "path does not exist"}
	}
	if errors.Is(err, fs.ErrPermission) {
		return &Error{Code: CodeDenied, Message: "path is not accessible"}
	}
	return &Error{Code: CodeInternal, Message: InternalErrorMsg}
}

// AllowPath puts p on the fs allow-list: p must be absolute; the list
// stores filepath.Clean(p) AND, when the path exists, its
// filepath.EvalSymlinks resolution. A later request is accepted only
// when its cleaned form AND its live symlink resolution are both on
// the list, so a symlink ALREADY IN PLACE when the request arrives
// cannot widen access (the rootwrite posture).
//
// It is deliberately not open-then-verify, and the limit of that is
// worth stating: pathAllowed resolves and the handler then opens, so a
// process running as the same user could still swap a component in
// between. That buys an attacker nothing here, a same-uid process can
// read the file directly, and the page, which is the actual attacker in
// this threat model, cannot create a symlink through any bridge method.
// The write path is unaffected either way: writeFileAtomic finishes
// with os.Rename, which does not follow a final symlink.
func (b *Battery) AllowPath(p string) error {
	if !filepath.IsAbs(p) {
		return errors.New("desktop: AllowPath requires an absolute path")
	}
	clean := filepath.Clean(p)
	b.allowMu.Lock()
	b.allowPaths[clean] = struct{}{}
	b.allowMu.Unlock()
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		b.allowMu.Lock()
		b.allowPaths[resolved] = struct{}{}
		b.allowMu.Unlock()
	} else {
		// p may not exist yet (a save dialog target): resolve the
		// deepest existing ancestor and append the missing tail, so
		// the resolution computed AFTER the file is created matches
		// the one stored now (the /var → /private/var case).
		if resolved := resolveExistingAncestor(p); resolved != "" && resolved != clean {
			b.allowMu.Lock()
			b.allowPaths[resolved] = struct{}{}
			b.allowMu.Unlock()
		}
	}
	return nil
}

// resolveExistingAncestor resolves p through symlinks as far as the
// filesystem currently allows: existing ancestors are resolved, the
// not-yet-existing tail is appended verbatim.
func resolveExistingAncestor(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	dir := filepath.Dir(p)
	if dir == p {
		return ""
	}
	r := resolveExistingAncestor(dir)
	if r == "" {
		return ""
	}
	return filepath.Join(r, filepath.Base(p))
}

// pathAllowed reports whether p may be touched: its cleaned absolute
// form must be on the list, and, when p currently resolves through
// symlinks, that resolution must be on the list too.
func (b *Battery) pathAllowed(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	b.allowMu.Lock()
	_, cleanOK := b.allowPaths[clean]
	b.allowMu.Unlock()
	if !cleanOK {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil && resolved != clean {
		b.allowMu.Lock()
		_, resolvedOK := b.allowPaths[resolved]
		b.allowMu.Unlock()
		return resolvedOK
	}
	return true
}

// writeFileAtomic writes data to path via a 0600 temp file in the same
// directory followed by os.Rename, so a crash never leaves a torn file
// and a replaced file keeps the temp's 0600 mode.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".gofastr-desktop-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

type fsTextOutput struct {
	Text string `json:"text"`
}

type fsStatOutput struct {
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

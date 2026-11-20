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

	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const fsReadLimit = 8 << 20

var errPathNotAllowed = &Error{Code: CodeDenied, Message: "path is not accessible"}

func (b *Battery) fsCapability() Capability {
	return Capability{
		Name:        "fs",
		Description: "Read and write files the user picked through a dialog in this session, and read and list under a folder the user picked.",
		Version:     1,
		Methods: []Method{
			{
				Name:        "readText",
				Description: "Reads a UTF-8 text file (at most 8 MiB) from the allow-list or under a picked folder.",
				Permission:  "fs:read",
				Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
				Output:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					req, derr := decodeFSPath(in)
					if derr != nil {
						return nil, derr
					}
					mapErr := fsError
					var f *os.File
					var err error
					if b.pathAllowed(req.Path) {
						f, err = os.Open(req.Path)
					} else if root, rel, ok := b.folderGrant(req.Path); ok {
						f, err = root.Open(rel)
						root.Close()
						mapErr = folderError
					} else {
						return nil, errPathNotAllowed
					}
					if err != nil {
						return nil, mapErr(err)
					}
					defer f.Close()
					data, err := io.ReadAll(io.LimitReader(f, fsReadLimit+1))
					if err != nil {
						return nil, mapErr(err)
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
				Description: "Returns size, modification time, and directory flag for an allow-listed path or one under a picked folder.",
				Permission:  "fs:read",
				Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
				Output: json.RawMessage(`{"type":"object","properties":{` +
					`"size":{"type":"integer"},"modTime":{"type":"string"},"isDir":{"type":"boolean"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					req, derr := decodeFSPath(in)
					if derr != nil {
						return nil, derr
					}
					var info fs.FileInfo
					var err error
					mapErr := fsError
					if b.pathAllowed(req.Path) {
						info, err = os.Stat(req.Path)
					} else if root, rel, ok := b.folderGrant(req.Path); ok {
						info, err = root.Stat(rel)
						root.Close()
						mapErr = folderError
					} else {
						return nil, errPathNotAllowed
					}
					if err != nil {
						return nil, mapErr(err)
					}
					return fsStatOutput{
						Size:    info.Size(),
						ModTime: info.ModTime().UTC().Format(time.RFC3339Nano),
						IsDir:   info.IsDir(),
					}, nil
				},
			},
			{
				Name:        "list",
				Description: "Lists a folder under a picked folder, the picked folder included: each entry's name and directory flag, sorted by name, at most 10000 (truncated is true past that).",
				Permission:  "fs:read",
				Input:       json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
				Output: json.RawMessage(`{"type":"object","properties":{` +
					`"entries":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"isDir":{"type":"boolean"}}}},` +
					`"truncated":{"type":"boolean"}}}`),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					req, derr := decodeFSPath(in)
					if derr != nil {
						return nil, derr
					}
					root, rel, ok := b.folderGrant(req.Path)
					if !ok {
						return nil, errPathNotAllowed
					}
					defer root.Close()
					out, lerr := listFolder(root, rel)
					if lerr != nil {
						return nil, lerr
					}
					return out, nil
				},
			},
		},
	}
}

type fsPathInput struct {
	Path string `json:"path"`
}

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
// filepath.EvalSymlinks resolution (for a missing path, its deepest
// existing ancestor's, with the tail appended).
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
		if resolved := resolveExistingAncestor(p); resolved != "" && resolved != clean {
			b.allowMu.Lock()
			b.allowPaths[resolved] = struct{}{}
			b.allowMu.Unlock()
		}
	}
	return nil
}

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

func (b *Battery) pathAllowed(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	b.allowMu.Lock()
	_, cleanOK := b.allowPaths[p]
	b.allowMu.Unlock()
	if !cleanOK {
		return false
	}
	if resolved := resolveExistingAncestor(p); resolved != p {
		b.allowMu.Lock()
		_, resolvedOK := b.allowPaths[resolved]
		b.allowMu.Unlock()
		return resolvedOK
	}
	return true
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".gofastr-desktop-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := fileperm.RestrictOpenFile(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := atomicfile.Replace(tmp, path); err != nil {
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

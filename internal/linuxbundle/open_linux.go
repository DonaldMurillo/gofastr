package linuxbundle

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Bundle is an opened, verified bundle directory.
type Bundle struct {
	Root        *os.Root
	Manifest    Manifest
	RawManifest []byte
}

// Close releases the bundle's directory handle.
func (b *Bundle) Close() error { return b.Root.Close() }

// Open verifies the bundle directory dir: a strict manifest naming the
// fixed five-file payload, every file's size and sha256, no unlisted
// entry, link or directory, an ELF executable for the manifest's
// architecture and a PNG icon. installed also admits the receipt and
// requires the owner-only modes install gives each payload file;
// requireHost refuses a bundle for another architecture than this one.
// Checksums detect changes, not who published the bundle.
func Open(dir string, installed, requireHost bool) (*Bundle, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// Source roots may be read-only/public, but never a symlink. Root confines
	// descendants; each inventory member is additionally refused if non-regular.
	st, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("bundle must be a directory, not a symlink")
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*Bundle, error) { root.Close(); return nil, e }
	raw, err := ReadFile(root, ManifestName, MaxManifest)
	if err != nil {
		return fail(err)
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return fail(err)
	}
	arch := strings.TrimPrefix(manifest.Platform, "linux-")
	if requireHost && arch != runtime.GOARCH {
		return fail(fmt.Errorf("bundle architecture %s does not match this host's %s", arch, runtime.GOARCH))
	}
	expected := ExpectedFiles(manifest.ID)
	if len(manifest.Files) != len(expected) {
		return fail(fmt.Errorf("bundle must contain the fixed five-file payload"))
	}
	seen := make(map[string]bool)
	for _, entry := range manifest.Files {
		mode, ok := expected[entry.Path]
		sum, e := hex.DecodeString(entry.SHA256)
		maxSize := int64(maxPayloadFile)
		if entry.Path == "bin/"+manifest.ID {
			maxSize = MaxExecutable
		}
		if !ok || seen[entry.Path] || mode != entry.Mode || entry.Size < 0 || entry.Size > maxSize || e != nil || len(sum) != sha256.Size || entry.SHA256 != strings.ToLower(entry.SHA256) {
			return fail(fmt.Errorf("invalid or duplicate bundle inventory entry %q", entry.Path))
		}
		seen[entry.Path] = true
		if err = VerifyFile(root, entry); err != nil {
			return fail(err)
		}
		if installed {
			if err = VerifyInstalledMode(root, entry.Path, InstalledMode(entry.Mode)); err != nil {
				return fail(err)
			}
		}
	}
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path == "." || path == "bin" {
			if !d.IsDir() {
				return fmt.Errorf("invalid bundle directory")
			}
			return nil
		}
		if (path == ManifestName || installed && path == ReceiptName || seen[path]) && d.Type().IsRegular() {
			return nil
		}
		return fmt.Errorf("unexpected bundle entry %q; no unlisted files, links, or directories are installed or removed", path)
	})
	if err != nil {
		return fail(err)
	}
	if err = checkExecutable(root, "bin/"+manifest.ID, arch); err != nil {
		return fail(fmt.Errorf("invalid bundle executable: %w", err))
	}
	icon, err := ReadFile(root, "icon.png", maxPayloadFile)
	if err != nil {
		return fail(err)
	}
	if err = ValidateIcon(icon); err != nil {
		return fail(err)
	}
	return &Bundle{Root: root, Manifest: manifest, RawManifest: raw}, nil
}

func checkExecutable(root *os.Root, name, arch string) error {
	exe, err := OpenRegular(root, name)
	if err != nil {
		return err
	}
	defer exe.Close()
	object, err := elf.NewFile(exe)
	if err != nil {
		return err
	}
	defer object.Close()
	expectedMachine := elf.EM_X86_64
	if arch == "arm64" {
		expectedMachine = elf.EM_AARCH64
	}
	entryInCode := false
	for _, program := range object.Progs {
		if program.Type == elf.PT_LOAD && program.Flags&elf.PF_X != 0 && object.Entry >= program.Vaddr && object.Entry-program.Vaddr < program.Memsz {
			entryInCode = true
		}
	}
	if object.Class != elf.ELFCLASS64 || object.Data != elf.ELFDATA2LSB || object.Machine != expectedMachine || (object.OSABI != elf.ELFOSABI_NONE && object.OSABI != elf.ELFOSABI_LINUX) || (object.Type != elf.ET_EXEC && object.Type != elf.ET_DYN) || object.Entry == 0 || !entryInCode {
		return fmt.Errorf("ELF executable does not match bundle platform or has no executable entry point")
	}
	return nil
}

// OpenInstalled opens the installed bundle directory dir for app id:
// Open with installed set, the owner-only modes of the directory, bin,
// the manifest and the receipt, and a receipt that names id and the
// manifest's exact bytes. The launcher and desktop entry digests are
// the caller's to check: they live outside dir.
func OpenInstalled(dir, id string) (*Bundle, Receipt, error) {
	b, err := Open(dir, true, false)
	if err != nil {
		return nil, Receipt{}, err
	}
	fail := func(e error) (*Bundle, Receipt, error) { b.Close(); return nil, Receipt{}, e }
	for _, target := range []struct {
		name string
		mode fs.FileMode
	}{{".", 0o700}, {"bin", 0o700}, {ManifestName, 0o600}, {ReceiptName, 0o600}} {
		if err = VerifyInstalledMode(b.Root, target.name, target.mode); err != nil {
			return fail(err)
		}
	}
	receipt, err := readReceipt(b.Root, b.Manifest, b.RawManifest, id)
	if err != nil {
		return fail(err)
	}
	return b, receipt, nil
}

func readReceipt(root *os.Root, m Manifest, rawManifest []byte, id string) (Receipt, error) {
	raw, err := ReadFile(root, ReceiptName, MaxReceipt)
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	if err = handler.UnmarshalStrict(raw, &receipt); err != nil {
		return Receipt{}, fmt.Errorf("invalid installation receipt: %w", err)
	}
	if receipt.SchemaVersion != receiptVersion || receipt.ID != id || m.ID != id || receipt.ManifestSHA256 != Digest(rawManifest) {
		return Receipt{}, fmt.Errorf("installation receipt does not match the bundle")
	}
	return receipt, nil
}

// ReadInstalled reads which app and version the installation directory
// dir holds, without hashing its payload: a manifest ParseManifest
// accepts, naming id, a receipt naming id and the manifest's sha256,
// and a regular bin/<id>. It is for the running app to know itself;
// Open and OpenInstalled are the checks of what is on disk.
func ReadInstalled(dir, id string) (Manifest, Receipt, error) {
	st, err := os.Lstat(dir)
	if err != nil {
		return Manifest{}, Receipt{}, err
	}
	if !st.IsDir() {
		return Manifest{}, Receipt{}, fmt.Errorf("the installation is not a directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Manifest{}, Receipt{}, err
	}
	defer root.Close()
	raw, err := ReadFile(root, ManifestName, MaxManifest)
	if err != nil {
		return Manifest{}, Receipt{}, err
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return Manifest{}, Receipt{}, err
	}
	receipt, err := readReceipt(root, m, raw, id)
	if err != nil {
		return Manifest{}, Receipt{}, err
	}
	exe, err := OpenRegular(root, "bin/"+id)
	if err != nil {
		return Manifest{}, Receipt{}, err
	}
	_ = exe.Close()
	return m, receipt, nil
}

// Populate copies src's payload and manifest into the empty directory
// dest at their installed modes, re-hashing every byte it writes. It
// returns the names it created, in order, so a caller can remove them
// last first on a later failure; on its own failure it has already
// removed nothing but the file it failed on.
func Populate(src *Bundle, dest *os.Root) ([]string, error) {
	var created []string
	if err := dest.Mkdir("bin", 0o700); err != nil {
		return created, err
	}
	created = append(created, "bin")
	for _, entry := range src.Manifest.Files {
		if err := CopyFile(src.Root, dest, entry); err != nil {
			return created, err
		}
		created = append(created, entry.Path)
	}
	if err := WriteFile(dest, ManifestName, src.RawManifest, 0o600); err != nil {
		return created, err
	}
	return append(created, ManifestName), nil
}

// OpenRegular opens name under root without following a final symlink or blocking on a FIFO, and refuses anything but a regular file.
func OpenRegular(root *os.Root, name string) (*os.File, error) {
	// Nonblocking plus no-follow also prevents a FIFO/symlink replacement between
	// validation and open from hanging or following the leaf.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("bundle/installation entry %q must be a regular file", name)
	}
	return f, nil
}

// ReadFile reads the regular file name under root, refusing one past limit bytes; limit is held to MaxExecutable.
func ReadFile(root *os.Root, name string, limit int64) ([]byte, error) {
	limit = min(max(limit, 0), MaxExecutable)
	f, err := OpenRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("entry %q exceeds its size limit", name)
	}
	return b, nil
}

// VerifyFile checks entry's size and sha256 under root.
func VerifyFile(root *os.Root, entry File) error {
	f, err := OpenRegular(root, entry.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	//gofastr:allow(intwrap) entry.Size is refused past maxSize when the manifest is read (Open)
	n, err := io.Copy(h, io.LimitReader(f, entry.Size+1))
	if err != nil {
		return err
	}
	if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
		return fmt.Errorf("bundle checksum or size mismatch for %q", entry.Path)
	}
	return nil
}

// CopyFile copies entry from source to a new file in dest at its installed mode and refuses bytes that no longer match the entry.
func CopyFile(source, dest *os.Root, entry File) (err error) {
	in, err := OpenRegular(source, entry.Path)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dest.OpenFile(entry.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, InstalledMode(entry.Mode))
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = dest.Remove(entry.Path)
		}
	}()
	h := sha256.New()
	//gofastr:allow(intwrap) entry.Size is refused past maxSize when the manifest is read (Open)
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, entry.Size+1))
	err = errors.Join(err, out.Close())
	if err != nil {
		return err
	}
	if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
		return fmt.Errorf("bundle changed while copying %q", entry.Path)
	}
	return nil
}

// VerifyInstalledMode refuses name under root unless this user owns it with exactly mode and no symlink, setuid, setgid or sticky bit.
func VerifyInstalledMode(root *os.Root, name string, mode fs.FileMode) error {
	st, err := root.Lstat(name)
	if err != nil {
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || st.Mode().Perm() != mode || st.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("owner or permissions changed for %q", name)
	}
	return nil
}

// OpenDirectory opens the absolute directory name through root-relative
// handles from /, creating missing parts owner-only when create is set.
// It refuses a symlink or non-directory anywhere in the chain, an
// ancestor DirectoryPolicy refuses, and a final directory not owned by
// this user or writable by group or others. Existing modes are never
// changed, and nothing here defends against a process running as this
// user.
func OpenDirectory(name string, create bool) (*os.Root, error) {
	if !filepath.IsAbs(name) {
		return nil, fmt.Errorf("installation path must be absolute")
	}
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(name), "/"), "/")
	for _, part := range parts {
		st, e := root.Lstat(part)
		if os.IsNotExist(e) && create {
			if e = root.Mkdir(part, 0700); e == nil {
				st, e = root.Lstat(part)
			}
		}
		if e != nil {
			root.Close()
			return nil, e
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, fmt.Errorf("refusing non-directory or symlink in installation path %q", name)
		}
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !DirectoryPolicy(stat.Uid, st.Mode(), false) {
			root.Close()
			return nil, fmt.Errorf("refusing foreign-owned or writable ancestor in installation path %q", name)
		}
		next, e := root.OpenRoot(part)
		root.Close()
		if e != nil {
			return nil, e
		}
		root = next
	}
	st, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !DirectoryPolicy(stat.Uid, st.Mode(), true) {
		root.Close()
		return nil, fmt.Errorf("installation directory %q must be owned by the current user and not writable by group/others", name)
	}
	return root, nil
}

// DirectoryPolicy reports whether a directory with this owner and mode
// may sit in an installation path: the final one must be this user's
// and not group or other writable; an ancestor may also be root's, and
// a sticky shared one such as /tmp is allowed. An owner can replace
// descendants even under 0755, hence the ownership rule.
func DirectoryPolicy(owner uint32, mode fs.FileMode, leaf bool) bool {
	uid := uint32(os.Geteuid())
	if leaf {
		return owner == uid && mode.Perm()&0022 == 0
	}
	return (owner == uid || owner == 0) && (mode.Perm()&0022 == 0 || mode&os.ModeSticky != 0)
}

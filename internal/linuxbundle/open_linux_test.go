package linuxbundle_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/linuxbundle"
	"github.com/DonaldMurillo/gofastr/internal/linuxbundle/linuxbundletest"
)

func TestOpenRefusesABadPayload(t *testing.T) {
	spec := linuxbundletest.Default("1.0.0")
	elf := linuxbundletest.ELF(spec.Arch, "")
	outside := append([]byte(nil), elf...)
	binary.LittleEndian.PutUint64(outside[24:], 0x500000) // entry past the one PF_X segment
	noexec := append([]byte(nil), elf...)
	binary.LittleEndian.PutUint32(noexec[64+4:], 4) // PF_R only
	for name, exe := range map[string][]byte{"an entry point outside the executable segment": outside, "no executable segment": noexec} {
		t.Run(name, func(t *testing.T) {
			dir := linuxbundletest.WriteSource(t, t.TempDir(), linuxbundletest.EntriesWithExecutable(spec, exe))
			if b, err := linuxbundle.Open(dir, false, true); err == nil {
				b.Close()
				t.Fatal("Open accepted the executable")
			}
		})
	}
	for name, icon := range map[string][]byte{"an icon wider than 4096": linuxbundletest.PNGSized(4097, 1), "an icon taller than 4096": linuxbundletest.PNGSized(1, 4097), "an icon that is not a PNG": []byte("GIF89a")} {
		t.Run(name, func(t *testing.T) {
			dir := linuxbundletest.WriteSource(t, t.TempDir(), linuxbundletest.EntriesWithIcon(spec, icon))
			if b, err := linuxbundle.Open(dir, false, true); err == nil {
				b.Close()
				t.Fatal("Open accepted the icon")
			}
		})
	}
	t.Run("a 4096 pixel icon is accepted", func(t *testing.T) {
		dir := linuxbundletest.WriteSource(t, t.TempDir(), linuxbundletest.EntriesWithIcon(spec, linuxbundletest.PNGSized(4096, 1)))
		b, err := linuxbundle.Open(dir, false, true)
		if err != nil {
			t.Fatal(err)
		}
		b.Close()
	})
}

func TestOpenInstalledRefusesAChangedInstallation(t *testing.T) {
	spec := linuxbundletest.Default("1.0.0")
	launcher, desktopEntry := []byte("l"), []byte("d")
	rewrite := func(t *testing.T, dir string, r linuxbundle.Receipt) {
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if err := root.Remove(linuxbundle.ReceiptName); err != nil {
			t.Fatal(err)
		}
		if err := linuxbundle.WriteReceipt(root, r); err != nil {
			t.Fatal(err)
		}
	}
	chmod := func(rel string, mode os.FileMode) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			if err := os.Chmod(filepath.Join(dir, rel), mode); err != nil {
				t.Fatal(err)
			}
		}
	}
	manifest := func(t *testing.T, dir string) []byte {
		raw, err := os.ReadFile(filepath.Join(dir, linuxbundle.ManifestName))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	cases := map[string]func(*testing.T, string){
		"the directory open to others":   chmod(".", 0o755),
		"bin open to others":             chmod("bin", 0o755),
		"the manifest readable by all":   chmod(linuxbundle.ManifestName, 0o644),
		"the receipt readable by all":    chmod(linuxbundle.ReceiptName, 0o644),
		"a payload file readable by all": chmod("README.txt", 0o644),
		"a receipt naming another app": func(t *testing.T, dir string) {
			rewrite(t, dir, linuxbundle.NewReceipt("dev.gofastr.other", manifest(t, dir), launcher, desktopEntry))
		},
		"a receipt for another manifest": func(t *testing.T, dir string) {
			rewrite(t, dir, linuxbundle.NewReceipt(spec.ID, []byte("{}"), launcher, desktopEntry))
		},
		"a receipt with an unknown field": func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, linuxbundle.ReceiptName), []byte(`{"schema_version":1,"extra":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			dir := linuxbundletest.Install(t, filepath.Join(t.TempDir(), "apps"), spec, launcher, desktopEntry)
			change(t, dir)
			if b, _, err := linuxbundle.OpenInstalled(dir, spec.ID); err == nil {
				b.Close()
				t.Fatal("OpenInstalled accepted it")
			}
		})
	}
	t.Run("an installation opened under another id", func(t *testing.T) {
		dir := linuxbundletest.Install(t, filepath.Join(t.TempDir(), "apps"), spec, launcher, desktopEntry)
		if b, _, err := linuxbundle.OpenInstalled(dir, "dev.gofastr.other"); err == nil {
			b.Close()
			t.Fatal("OpenInstalled accepted another app's id")
		}
	})
	t.Run("an unchanged installation", func(t *testing.T) {
		dir := linuxbundletest.Install(t, filepath.Join(t.TempDir(), "apps"), spec, launcher, desktopEntry)
		b, r, err := linuxbundle.OpenInstalled(dir, spec.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		if r.LauncherSHA256 != linuxbundle.Digest(launcher) {
			t.Fatal("receipt does not carry the launcher digest")
		}
	})
}

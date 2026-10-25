package desktopcmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/devcmd"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// desktopKeygenFlags carries `desktop keygen`'s flags.
type desktopKeygenFlags struct {
	out string
}

// parseDesktopKeygenFlags parses the keygen flag list.
func parseDesktopKeygenFlags(args []string) desktopKeygenFlags {
	var f desktopKeygenFlags
	for i := 0; i < len(args); i++ {
		name, value, hasValue := devcmd.SplitFlagEqual(args[i])
		switch name {
		case "-o", "--o", "-out", "--out":
			if hasValue {
				f.out = value
			} else if i+1 < len(args) {
				f.out = args[i+1]
				i++
			}
		case "--help", "-h":
			printDesktopUsage()
			cli.OsExit(0)
		default:
			cli.Fail("Unknown keygen flag: %s", args[i])
			cli.OsExit(1)
		}
	}
	return f
}

// runDesktopKeygen writes the update signing pair: <path> holding
// hex(seed)+hex(public) (0600) and <path>.pub holding hex(public).
// It refuses to overwrite an existing private key.
func runDesktopKeygen(args []string) {
	f := parseDesktopKeygenFlags(args)
	if f.out == "" {
		cli.Fail("keygen needs -o <path> (where the private key file goes)")
		printDesktopUsage()
		cli.OsExit(1)
		return
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		cli.Fail("keygen failed: %v", err)
		cli.OsExit(1)
		return
	}
	body := hex.EncodeToString(priv.Seed()) + hex.EncodeToString(pub)
	if _, err := os.Stat(f.out); err == nil {
		cli.Fail("%s already exists; keygen refuses to overwrite a private key (move it aside or pick another -o)", f.out)
		cli.OsExit(1)
		return
	}
	if err := fileperm.WriteOwnerOnly(f.out, []byte(body)); err != nil {
		cli.Fail("writing %s failed: %v", f.out, err)
		cli.OsExit(1)
		return
	}
	pubPath := f.out + ".pub"
	if err := fileperm.WriteOwnerOnly(pubPath, []byte(hex.EncodeToString(pub))); err != nil {
		cli.Fail("writing %s failed: %v", pubPath, err)
		cli.OsExit(1)
		return
	}
	cli.Success("update signing key written to %s", f.out)
	cli.Success("public key written to %s", pubPath)
	cli.Info("put %s's contents into desktop.UpdateConfig.PublicKey", pubPath)
}

package main

// The `gofastr desktop keygen` verb: mint the ed25519 pair the update
// feed is signed and verified with. Pure Go (crypto/ed25519 +
// crypto/rand); the private file is the key the `feed` verb reads.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"

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
		name, value, hasValue := splitFlagEqual(args[i])
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
			osExit(0)
		default:
			fail("Unknown keygen flag: %s", args[i])
			osExit(1)
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
		fail("keygen needs -o <path> (where the private key file goes)")
		printDesktopUsage()
		osExit(1)
		return
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail("keygen failed: %v", err)
		osExit(1)
		return
	}
	body := hex.EncodeToString(priv.Seed()) + hex.EncodeToString(pub)
	if _, err := os.Stat(f.out); err == nil {
		fail("%s already exists; keygen refuses to overwrite a private key (move it aside or pick another -o)", f.out)
		osExit(1)
		return
	}
	if err := fileperm.WriteOwnerOnly(f.out, []byte(body)); err != nil {
		fail("writing %s failed: %v", f.out, err)
		osExit(1)
		return
	}
	pubPath := f.out + ".pub"
	if err := fileperm.WriteOwnerOnly(pubPath, []byte(hex.EncodeToString(pub))); err != nil {
		fail("writing %s failed: %v", pubPath, err)
		osExit(1)
		return
	}
	success("update signing key written to %s", f.out)
	success("public key written to %s", pubPath)
	info("put %s's contents into desktop.UpdateConfig.PublicKey", pubPath)
}

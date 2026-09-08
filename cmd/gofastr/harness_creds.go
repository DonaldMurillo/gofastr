package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/credstore"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// runHarnessCreds dispatches `gofastr harness creds <subcommand>`.
//
// Subcommands:
//
//	add  <provider> <account> <secret>   Store a credential ("-" reads it from stdin; GOFASTR_HARNESS_SECRET env also works).
//	list                                 List stored providers/accounts (no secrets).
//	delete <provider> <account>          Remove a stored credential.
func runHarnessCreds(args []string) {
	if len(args) == 0 {
		fail("Subcommand required.")
		info("Usage: gofastr harness creds [add|list|delete]")
		osExit(1)
		return
	}
	switch args[0] {
	case "add":
		runHarnessCredsAdd(args[1:])
	case "list":
		runHarnessCredsList(args[1:])
	case "delete", "del", "rm":
		runHarnessCredsDelete(args[1:])
	default:
		fail("Unknown creds subcommand %q.", args[0])
		info("Try: add, list, or delete.")
		osExit(1)
	}
}

// runHarnessCredsAdd stores one credential in the harness credstore.
//
//	gofastr harness creds add <provider> <account> <secret>
//
// The secret argument accepts the conventional "-" marker: the real
// secret is then read from GOFASTR_HARNESS_SECRET or piped stdin (env
// first), never stored as the literal marker. A literal secret keeps
// working (documented interface) but draws a one-line warning: argv is
// ps/procfs world-readable, the exact local observer the encrypted
// store exists to defend against.
//
// Examples:
//
//	gofastr harness creds add openrouter default -   # secret on stdin
//	echo "$KEY" | gofastr harness creds add zai default -
//	GOFASTR_HARNESS_SECRET=... gofastr harness creds add zai default -
//	gofastr harness creds add zai default <api-key>  # legacy argv form
//
// Key resolution order (first wins):
//  1. GOFASTR_HARNESS_MACHINE_KEY env var (32-byte hex/base64/raw key).
//  2. GOFASTR_HARNESS_PASSPHRASE env var.
//
// With neither set the store refuses to open (no default passphrase).
func runHarnessCredsAdd(args []string) {
	if len(args) < 3 {
		fail("Usage: gofastr harness creds add <provider> <account> <secret>")
		osExit(1)
		return
	}
	provider, account := args[0], args[1]
	secret, err := resolveCredSecret(args[2])
	if err != nil {
		fail("Cannot resolve secret: %v", err)
		osExit(1)
		return
	}

	store, err := openCredstore()
	if err != nil {
		fail("Cannot open credstore: %v", err)
		osExit(1)
		return
	}
	if err := store.Put(provider, account, secret); err != nil {
		fail("Cannot store credential: %v", err)
		osExit(1)
		return
	}
	success("Stored credential for %s/%s", provider, account)
}

// resolveCredSecret resolves the `creds add` secret argument. "-" is the
// conventional stdin marker: GOFASTR_HARNESS_SECRET wins, then piped
// stdin; an empty resolution is refused rather than storing "-" or "".
// A literal value is returned as-is after a one-line argv warning.
func resolveCredSecret(literal string) (string, error) {
	if literal != "-" {
		fmt.Fprintln(os.Stderr, "warning: a secret passed as an argument is visible to every local process via ps; prefer GOFASTR_HARNESS_SECRET or `add <provider> <account> -` (reads the secret from stdin)")
		return literal, nil
	}
	if env := os.Getenv("GOFASTR_HARNESS_SECRET"); env != "" {
		return env, nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read secret from stdin: %w", err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("`-` marker given but no secret arrived on stdin (or GOFASTR_HARNESS_SECRET); refusing to store the marker")
	}
	return secret, nil
}

// runHarnessCredsList prints every stored provider/account pair (no secrets).
func runHarnessCredsList(args []string) {
	_ = args
	store, err := openCredstore()
	if err != nil {
		fail("Cannot open credstore: %v", err)
		osExit(1)
		return
	}
	entries, err := store.List()
	if err != nil {
		fail("Cannot list credentials: %v", err)
		osExit(1)
		return
	}
	if len(entries) == 0 {
		info("No credentials stored.")
		return
	}
	// Sort for stable output.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Provider != entries[j].Provider {
			return entries[i].Provider < entries[j].Provider
		}
		return entries[i].Account < entries[j].Account
	})
	fmt.Println()
	fmt.Printf("  %-20s  %s\n", "PROVIDER", "ACCOUNT")
	fmt.Printf("  %-20s  %s\n", "--------", "-------")
	for _, e := range entries {
		fmt.Printf("  %-20s  %s\n", e.Provider, e.Account)
	}
	fmt.Println()
}

// runHarnessCredsDelete removes a stored credential.
//
//	gofastr harness creds delete <provider> <account>
func runHarnessCredsDelete(args []string) {
	if len(args) < 2 {
		fail("Usage: gofastr harness creds delete <provider> <account>")
		osExit(1)
		return
	}
	provider, account := args[0], args[1]
	store, err := openCredstore()
	if err != nil {
		fail("Cannot open credstore: %v", err)
		osExit(1)
		return
	}
	if err := store.Delete(provider, account); err != nil {
		fail("Cannot delete credential: %v", err)
		osExit(1)
		return
	}
	success("Deleted credential for %s/%s", provider, account)
}

// openCredstore resolves the XDG config path and derives the credstore
// key using the same priority order as runHarness:
//  1. GOFASTR_HARNESS_MACHINE_KEY (CI / headless path)
//  2. GOFASTR_HARNESS_PASSPHRASE
//
// With neither set it fails closed (noCredstoreKeyHelp); there is no
// default passphrase.
func openCredstore() (*credstore.EncryptedFileStore, error) {
	xdgConfig, err := resolveHarnessXDGConfig()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(xdgConfig, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	key, err := deriveCredstoreKeyFromEnv(xdgConfig)
	if err != nil {
		return nil, err
	}
	credPath := filepath.Join(xdgConfig, "creds.enc")
	return credstore.NewEncryptedFileStore(credPath, key)
}

// resolveHarnessXDGConfig returns ~/.config/gofastr/harness, honouring
// XDG_CONFIG_HOME when it is set (standard XDG Base Dir spec).
func resolveHarnessXDGConfig() (string, error) {
	if xch := os.Getenv("XDG_CONFIG_HOME"); xch != "" {
		return filepath.Join(xch, "gofastr", "harness"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "gofastr", "harness"), nil
}

// noCredstoreKeyHelp is the single fail-closed refusal shared by every
// CLI path that opens the credential store (harness, harness mcp,
// harness creds). The repo shipped a hard-coded default passphrase
// here until the 2026-09-04 red-probe round; a constant published in
// the source tree is not encryption, so with neither a passphrase nor
// a machine key configured the store refuses to open and the harness
// refuses to boot. The message names the two ways to provide a key.
const noCredstoreKeyHelp = "no credential-store key configured: set GOFASTR_HARNESS_PASSPHRASE, or provide a 32-byte key via GOFASTR_HARNESS_MACHINE_KEY"

// deriveCredstoreKeyFromEnv picks the key using the same policy as the
// main runHarness boot path:
//  1. GOFASTR_HARNESS_MACHINE_KEY
//  2. GOFASTR_HARNESS_PASSPHRASE
//
// With neither set it fails closed (see noCredstoreKeyHelp): there is
// deliberately no default passphrase.
func deriveCredstoreKeyFromEnv(xdgConfig string) ([]byte, error) {
	if mk, err := machineKeyFromEnv(); err != nil {
		return nil, fmt.Errorf("GOFASTR_HARNESS_MACHINE_KEY: %w", err)
	} else if len(mk) == 32 {
		return mk, nil
	}

	pass := os.Getenv("GOFASTR_HARNESS_PASSPHRASE")
	if pass == "" {
		return nil, errors.New(noCredstoreKeyHelp)
	}

	saltPath := filepath.Join(xdgConfig, "salt")
	salt, err := credsReadOrCreateSalt(saltPath)
	if err != nil {
		return nil, fmt.Errorf("salt: %w", err)
	}
	return credstore.DeriveKey([]byte(pass), salt), nil
}

// credsReadOrCreateSalt reads the salt from path, creating a new random
// salt if the file is absent or too short. This mirrors the behaviour of
// the main harness boot path so both share the same salt file.
func credsReadOrCreateSalt(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil && len(data) >= 16 {
		return data, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	// WriteOwnerOnly, not os.WriteFile: the mode must hold on overwrite
	// too, and this salt derives the credstore key.
	if err := fileperm.WriteOwnerOnly(path, salt); err != nil {
		return nil, fmt.Errorf("write salt: %w", err)
	}
	return salt, nil
}

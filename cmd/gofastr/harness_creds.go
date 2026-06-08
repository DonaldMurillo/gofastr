package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider/credstore"
)

// runHarnessCreds dispatches `gofastr harness creds <subcommand>`.
//
// Subcommands:
//
//	add  <provider> <account> <secret>   Store a credential.
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
// Examples:
//
//	gofastr harness creds add openrouter default sk-or-...
//	gofastr harness creds add zai default <api-key>
//
// Key resolution order (first wins):
//  1. GOFASTR_HARNESS_MACHINE_KEY env var (32-byte hex/base64/raw key).
//  2. GOFASTR_HARNESS_PASSPHRASE env var.
//  3. Default passphrase (suitable for dev only; warns loudly).
func runHarnessCredsAdd(args []string) {
	if len(args) < 3 {
		fail("Usage: gofastr harness creds add <provider> <account> <secret>")
		osExit(1)
		return
	}
	provider, account, secret := args[0], args[1], args[2]

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
//  3. default passphrase (warns)
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

// deriveCredstoreKeyFromEnv picks the key using the same policy as the
// main runHarness boot path:
//  1. GOFASTR_HARNESS_MACHINE_KEY
//  2. GOFASTR_HARNESS_PASSPHRASE
//  3. hard-coded dev default (warns)
func deriveCredstoreKeyFromEnv(xdgConfig string) ([]byte, error) {
	if mk, err := machineKeyFromEnv(); err != nil {
		return nil, fmt.Errorf("GOFASTR_HARNESS_MACHINE_KEY: %w", err)
	} else if len(mk) == 32 {
		return mk, nil
	}

	pass := os.Getenv("GOFASTR_HARNESS_PASSPHRASE")
	if pass == "" {
		warn("Using default credstore passphrase. Set GOFASTR_HARNESS_PASSPHRASE to silence this warning.")
		pass = "harness-default-passphrase-change-me"
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
	if err := os.WriteFile(path, salt, 0o600); err != nil {
		return nil, fmt.Errorf("write salt: %w", err)
	}
	return salt, nil
}

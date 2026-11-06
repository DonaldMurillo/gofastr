package initcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitChecksAgentPathsBeforeWrites(t *testing.T) {
	for _, obstacle := range []string{"detail", "skill"} {
		t.Run(obstacle, func(t *testing.T) {
			inPlaceDir(t, "preflight-app")
			originals := map[string]string{".env": "DATABASE_URL=file:keep.db\nJWT_SECRET=keep\n", "main.go": "package main\n", "AGENTS.md": "My instructions\n"}
			for name, body := range originals {
				if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if obstacle == "detail" {
				if err := os.Mkdir(".agents", 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(".agents", "battery-auth.md"), []byte("hand-written\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(t.TempDir(), ".claude"); err != nil {
				t.Fatal(err)
			}
			if code := runInitCode(t, ".", "--force", "--no-entity"); code != 1 {
				t.Fatalf("refused init exit = %d, want 1", code)
			}
			for name, want := range originals {
				got, err := os.ReadFile(name)
				if err != nil || string(got) != want {
					t.Errorf("refused init changed %s (%v)", name, err)
				}
			}
		})
	}
}

package generatecmd

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

func runGenerateWatch(args []string) {
	options, err := appgen.ParseGenerateOptions(args)
	if err != nil {
		appgen.ReportGenerateFailure(args, err)
		cli.OsExit(2)
		return
	}
	asJSON := slices.Contains(args, "--json")
	if asJSON {
		cli.Fprintln(os.Stderr, "Watching generator inputs: Ctrl-C to stop.")
	} else {
		cli.Info("Watching generator inputs: Ctrl-C to stop.")
	}

	runOnce(args)

	lastHash := hashGenerateInputs(options)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		h := hashGenerateInputs(options)
		if h == lastHash {
			continue
		}
		lastHash = h
		if asJSON {
			cli.Fprintln(os.Stderr, "Detected codegen input change: regenerating...")
		} else {
			fmt.Printf("\033[2J\033[H")
			cli.Info("Detected codegen input change: regenerating...")
		}
		runOnce(args)
	}
}

func runOnce(args []string) {
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--watch" {
			continue
		}
		filtered = append(filtered, a)
	}
	exe, err := os.Executable()
	if err != nil {
		appgen.ReportGenerateFailure(args, fmt.Errorf("generate failed: %w", err))
		return
	}
	cmd := exec.Command(exe, append([]string{"generate"}, filtered...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		var exited *exec.ExitError
		if slices.Contains(args, "--json") && errors.As(err, &exited) {
			cli.Fprintf(os.Stderr, "generate failed: %v\n", err)
		} else {
			appgen.ReportGenerateFailure(args, fmt.Errorf("generate failed: %w", err))
		}
	}
}

func hashEntitiesDir(dir string) string {
	var paths []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintln(h, p)
		buf, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		h.Write(buf)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func hashGenerateInputs(options appgen.GenerateOptions) string {
	h := sha256.New()
	if options.ConfigPath != "" {
		hashFileInto(h, options.ConfigPath)
	}
	projectDir := "."
	discovery, err := appgen.DiscoverGenerateConfig(options)
	if err == nil && discovery.Found && strings.TrimSpace(discovery.ProjectDir) != "" {
		projectDir = discovery.ProjectDir
	}
	if absolute, pathErr := filepath.Abs(projectDir); pathErr == nil {
		if _, moduleRoot := project.FindEnclosingGoMod(absolute); moduleRoot != "" {
			hashFileInto(h, filepath.Join(moduleRoot, "go.mod"))
		}
	}
	hashEntityDecls(h, filepath.Join(projectDir, "entities"))
	if err == nil && discovery.Found {
		hashFileInto(h, discovery.Path)
		styleInputs, styleErr := appgen.GenerateStyleInputs(projectDir, discovery.Config.Codegen.Generators)
		if styleErr != nil {
			fmt.Fprintln(h, styleErr)
		}
		for _, path := range styleInputs {
			hashFileInto(h, path)
		}
		for _, ext := range discovery.Config.Codegen.Extensions {
			if len(ext.Command) > 0 && filepath.Base(ext.Command[0]) != ext.Command[0] {
				hashFileInto(h, projectRelativePath(projectDir, ext.Command[0]))
			}
		}
		for _, gen := range discovery.Config.Codegen.Generators {
			if (gen.Name == "cli" || gen.Name == "sdk") && gen.Extension == "" {
				if doc, ok := gen.Config["from_openapi"].(string); ok && strings.TrimSpace(doc) != "" {
					hashFileInto(h, projectRelativePath(projectDir, strings.TrimSpace(doc)))
				}
			}
			switch strings.ToLower(gen.Source.Type) {
			case "json_file":
				hashFileInto(h, projectRelativePath(projectDir, gen.Source.Path))
			case "json_dir":
				fmt.Fprintln(h, hashEntitiesDir(projectRelativePath(projectDir, gen.Source.Path)))
			}
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func hashFileInto(h interface{ Write([]byte) (int, error) }, path string) {
	fmt.Fprintln(h, path)
	data, err := os.ReadFile(path)
	if err == nil {
		_, _ = h.Write(data)
	}
}

func projectRelativePath(projectDir, path string) string {
	if filepath.IsAbs(path) || strings.TrimSpace(projectDir) == "" || projectDir == "." {
		return path
	}
	return filepath.Join(projectDir, path)
}

func hashEntityDecls(h interface{ Write([]byte) (int, error) }, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !e.Type().IsRegular() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		hashFileInto(h, filepath.Join(dir, e.Name()))
	}
}

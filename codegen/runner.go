package codegen

import (
	"context"
	"fmt"
	"io"
)

// Registry holds available in-process generators and extensions.
type Registry struct {
	generators map[string]Generator
	extensions map[string]Extension
}

// NewRegistry creates an empty generator/extension registry.
func NewRegistry() *Registry {
	return &Registry{
		generators: map[string]Generator{},
		extensions: map[string]Extension{},
	}
}

// RegisterGenerator adds one in-process generator by name.
func (r *Registry) RegisterGenerator(gen Generator) error {
	if gen == nil || gen.Name() == "" {
		return fmt.Errorf("codegen: generator name is required")
	}
	if _, exists := r.generators[gen.Name()]; exists {
		return fmt.Errorf("codegen: generator %q already registered", gen.Name())
	}
	r.generators[gen.Name()] = gen
	return nil
}

// RegisterExtension adds one in-process extension by name.
func (r *Registry) RegisterExtension(ext Extension) error {
	if ext == nil || ext.Name() == "" {
		return fmt.Errorf("codegen: extension name is required")
	}
	if _, exists := r.extensions[ext.Name()]; exists {
		return fmt.Errorf("codegen: extension %q already registered", ext.Name())
	}
	r.extensions[ext.Name()] = ext
	return nil
}

// RegisterCommandExtensions registers command-backed extensions from config.
func (r *Registry) RegisterCommandExtensions(cfg CodegenConfig, stderrWriter io.Writer) error {
	for _, ext := range cfg.Extensions {
		if _, exists := r.extensions[ext.Name]; exists {
			continue
		}
		if len(ext.Command) == 0 {
			return fmt.Errorf("codegen: extension %q command is required", ext.Name)
		}
		if err := r.RegisterExtension(NewCommandExtension(ext.Name, ext.Command, stderrWriter)); err != nil {
			return err
		}
	}
	return nil
}

// Run executes every configured generator and extension into one FileSet.
func (r *Registry) Run(ctx context.Context, projectDir string, cfg Config) (*Context, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if projectDir == "" {
		projectDir = "."
	}
	genCtx := &Context{
		ProjectDir: projectDir,
		Config:     cfg,
		Metadata:   map[string]any{"protocol_version": ProtocolVersion},
		Inputs:     map[string]any{},
		Files:      NewFileSet(),
	}
	exts := map[string]ExtensionConfig{}
	for _, ext := range cfg.Codegen.Extensions {
		exts[ext.Name] = ext
	}
	for i, gen := range cfg.Codegen.Generators {
		runGen := gen
		if runGen.ID == "" {
			runGen.ID = generatorInputKey(gen, i)
		}
		source, err := LoadSource(projectDir, runGen.Source)
		if err != nil {
			return genCtx, fmt.Errorf("generator %q source: %w", runGen.Name, err)
		}
		genCtx.Inputs[runGen.ID] = source
		if _, exists := genCtx.Inputs[runGen.Name]; !exists {
			genCtx.Inputs[runGen.Name] = source
		}
		if runGen.Extension != "" {
			extCfg, ok := exts[runGen.Extension]
			if !ok {
				extCfg = ExtensionConfig{Name: runGen.Extension}
			}
			ext, ok := r.extensions[runGen.Extension]
			if !ok {
				return genCtx, fmt.Errorf("extension %q is not registered", runGen.Extension)
			}
			for _, phase := range []string{"load", "validate", "render", "finalize"} {
				res, err := ext.RunPhase(ctx, phase, genCtx, runGen, extCfg)
				if err != nil {
					return genCtx, fmt.Errorf("extension %q phase %s: %w", runGen.Extension, phase, err)
				}
				if err := applyExtensionResponse(genCtx, runGen, runGen.Extension, res); err != nil {
					return genCtx, err
				}
			}
			continue
		}
		builtin, ok := r.generators[runGen.Name]
		if !ok {
			return genCtx, fmt.Errorf("generator %q is not registered", runGen.Name)
		}
		files, err := builtin.Generate(ctx, genCtx, runGen)
		if err != nil {
			return genCtx, fmt.Errorf("generator %q: %w", runGen.Name, err)
		}
		if err := addGeneratedFiles(genCtx.Files, runGen.Output, runGen.Name, files); err != nil {
			return genCtx, err
		}
	}
	return genCtx, nil
}

func applyExtensionResponse(genCtx *Context, gen GeneratorConfig, owner string, res ExtensionResponse) error {
	genCtx.Diagnostics = append(genCtx.Diagnostics, res.Diagnostics...)
	for _, path := range res.Deletes {
		prefixed, err := prefixPath(gen.Output, path)
		if err != nil {
			return err
		}
		if err := genCtx.Files.DeleteOwned(prefixed, owner); err != nil {
			return err
		}
	}
	return addGeneratedFiles(genCtx.Files, gen.Output, owner, res.Files)
}

func addGeneratedFiles(files *FileSet, output, owner string, generated []GeneratedFile) error {
	for _, file := range generated {
		path, err := prefixPath(output, file.Path)
		if err != nil {
			return err
		}
		file.Path = path
		file.Owner = owner
		if err := files.Add(file); err != nil {
			return err
		}
	}
	return nil
}

// CleanEnabled reports whether clean mode is enabled, defaulting to true.
func CleanEnabled(cfg CodegenConfig) bool {
	if cfg.Clean == nil {
		return true
	}
	return *cfg.Clean
}

func generatorInputKey(gen GeneratorConfig, index int) string {
	if gen.ID != "" {
		return gen.ID
	}
	return fmt.Sprintf("%s#%d", gen.Name, index)
}

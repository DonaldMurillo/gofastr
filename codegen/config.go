package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// Discovery is the result of looking for a codegen configuration file.
type Discovery struct {
	Path       string
	ProjectDir string
	Config     Config
	Found      bool
}

// DiscoverConfig finds the project codegen config. Dedicated codegen config
// files win; gofastr.yml is only considered when it has a codegen section.
func DiscoverConfig(projectDir string) (Discovery, error) {
	dirs, err := searchDirs(projectDir)
	if err != nil {
		return Discovery{}, err
	}
	for _, dir := range dirs {
		for _, name := range []string{"gofastr.codegen.yml", "gofastr.codegen.yaml"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				cfg, err := LoadConfig(path)
				if err != nil {
					return Discovery{}, err
				}
				return Discovery{Path: path, ProjectDir: dir, Config: cfg, Found: true}, nil
			} else if err != nil && !os.IsNotExist(err) {
				return Discovery{}, err
			}
		}
	}
	for _, dir := range dirs {
		for _, name := range []string{"gofastr.yml", "gofastr.yaml"} {
			path := filepath.Join(dir, name)
			has, err := HasCodegenSection(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return Discovery{}, err
			}
			if !has {
				continue
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				return Discovery{}, err
			}
			return Discovery{Path: path, ProjectDir: dir, Config: cfg, Found: true}, nil
		}
	}
	return Discovery{}, nil
}

func searchDirs(projectDir string) ([]string, error) {
	start, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for {
		dirs = append(dirs, start)
		parent := filepath.Dir(start)
		if parent == start {
			break
		}
		start = parent
	}
	return dirs, nil
}

// HasCodegenSection reports whether path has a top-level codegen key.
func HasCodegenSection(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	node, err := coreyaml.Parse(string(data))
	if err != nil {
		return false, err
	}
	return node.Kind == coreyaml.Map && node.Map["codegen"] != nil, nil
}

// LoadConfig reads and validates a YAML codegen configuration file.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	node, err := coreyaml.Parse(string(data))
	if err != nil {
		return Config{}, err
	}
	cfg, err := DecodeConfig(node)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// DecodeConfig decodes a parsed YAML node into Config.
func DecodeConfig(node *coreyaml.Node) (Config, error) {
	root, err := expectMap(node, "config")
	if err != nil {
		return Config{}, err
	}
	version, err := optionalIntValue(root["version"], "version")
	if err != nil {
		return Config{}, err
	}
	cfg := Config{Version: version}
	if root["version"] == nil {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return Config{}, fmt.Errorf("config.version %d is not supported", cfg.Version)
	}
	if root["codegen"] == nil {
		return Config{}, fmt.Errorf("codegen section is required")
	}
	cg, err := decodeCodegen(root["codegen"])
	if err != nil {
		return Config{}, err
	}
	cfg.Codegen = cg
	return cfg, nil
}

func decodeCodegen(node *coreyaml.Node) (CodegenConfig, error) {
	m, err := expectMap(node, "codegen")
	if err != nil {
		return CodegenConfig{}, err
	}
	allowed := map[string]bool{"output": true, "clean": true, "generators": true, "extensions": true}
	if err := rejectUnknownKeys(m, allowed, "codegen"); err != nil {
		return CodegenConfig{}, err
	}
	output, err := optionalStringValue(m["output"], "codegen.output")
	if err != nil {
		return CodegenConfig{}, err
	}
	cfg := CodegenConfig{Output: output}
	if cfg.Output == "" {
		cfg.Output = "gen"
	}
	if m["clean"] != nil {
		v, err := requiredBoolValue(m["clean"], "codegen.clean")
		if err != nil {
			return CodegenConfig{}, err
		}
		cfg.Clean = &v
	}
	gens, err := decodeGenerators(m["generators"])
	if err != nil {
		return CodegenConfig{}, err
	}
	cfg.Generators = gens
	exts, err := decodeExtensions(m["extensions"])
	if err != nil {
		return CodegenConfig{}, err
	}
	cfg.Extensions = exts
	if err := validateConfig(cfg, true); err != nil {
		return CodegenConfig{}, err
	}
	return cfg, nil
}

func decodeGenerators(node *coreyaml.Node) ([]GeneratorConfig, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "codegen.generators")
	if err != nil {
		return nil, err
	}
	out := make([]GeneratorConfig, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("codegen.generators[%d]", i))
		if err != nil {
			return nil, err
		}
		label := fmt.Sprintf("codegen.generators[%d]", i)
		allowed := map[string]bool{"id": true, "name": true, "extension": true, "source": true, "output": true, "config": true}
		if err := rejectUnknownKeys(m, allowed, label); err != nil {
			return nil, err
		}
		src, err := decodeSource(m["source"], label+".source")
		if err != nil {
			return nil, err
		}
		id, err := optionalStringValue(m["id"], label+".id")
		if err != nil {
			return nil, err
		}
		name, err := optionalStringValue(m["name"], label+".name")
		if err != nil {
			return nil, err
		}
		extension, err := optionalStringValue(m["extension"], label+".extension")
		if err != nil {
			return nil, err
		}
		output, err := optionalStringValue(m["output"], label+".output")
		if err != nil {
			return nil, err
		}
		out = append(out, GeneratorConfig{
			ID:        id,
			Name:      name,
			Extension: extension,
			Source:    src,
			Output:    output,
			Config:    mapValue(m["config"]),
		})
	}
	return out, nil
}

func decodeSource(node *coreyaml.Node, label string) (SourceConfig, error) {
	if node == nil {
		return SourceConfig{}, nil
	}
	m, err := expectMap(node, label)
	if err != nil {
		return SourceConfig{}, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"type": true, "path": true, "config": true}, label); err != nil {
		return SourceConfig{}, err
	}
	sourceType, err := optionalStringValue(m["type"], label+".type")
	if err != nil {
		return SourceConfig{}, err
	}
	path, err := optionalStringValue(m["path"], label+".path")
	if err != nil {
		return SourceConfig{}, err
	}
	return SourceConfig{
		Type:   sourceType,
		Path:   path,
		Config: mapValue(m["config"]),
	}, nil
}

func decodeExtensions(node *coreyaml.Node) ([]ExtensionConfig, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "codegen.extensions")
	if err != nil {
		return nil, err
	}
	out := make([]ExtensionConfig, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("codegen.extensions[%d]", i))
		if err != nil {
			return nil, err
		}
		label := fmt.Sprintf("codegen.extensions[%d]", i)
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "command": true, "config": true}, label); err != nil {
			return nil, err
		}
		command, err := requiredStringListValue(m["command"], label+".command")
		if err != nil {
			return nil, err
		}
		name, err := optionalStringValue(m["name"], label+".name")
		if err != nil {
			return nil, err
		}
		out = append(out, ExtensionConfig{
			Name:    name,
			Command: command,
			Config:  mapValue(m["config"]),
		})
	}
	return out, nil
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return Config{}, fmt.Errorf("config.version %d is not supported", cfg.Version)
	}
	if cfg.Codegen.Output == "" {
		cfg.Codegen.Output = "gen"
	}
	if err := validateConfig(cfg.Codegen, false); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateConfig(cfg CodegenConfig, requireExtensionCommands bool) error {
	if strings.TrimSpace(cfg.Output) == "" {
		return fmt.Errorf("codegen.output must not be empty")
	}
	seenGeneratorIDs := map[string]bool{}
	for i, gen := range cfg.Generators {
		if gen.Name == "" {
			return fmt.Errorf("codegen.generators[%d].name is required", i)
		}
		if gen.ID != "" {
			if seenGeneratorIDs[gen.ID] {
				return fmt.Errorf("duplicate generator id %q", gen.ID)
			}
			seenGeneratorIDs[gen.ID] = true
		}
		if gen.Extension != "" && gen.Source.Type == "" {
			return fmt.Errorf("codegen.generators[%d].source.type is required for extension generator %q", i, gen.Name)
		}
	}
	seenExtensions := map[string]bool{}
	for i, ext := range cfg.Extensions {
		if ext.Name == "" {
			return fmt.Errorf("codegen.extensions[%d].name is required", i)
		}
		if seenExtensions[ext.Name] {
			return fmt.Errorf("duplicate extension %q", ext.Name)
		}
		seenExtensions[ext.Name] = true
		if requireExtensionCommands && len(ext.Command) == 0 {
			return fmt.Errorf("codegen.extensions[%d].command is required", i)
		}
	}
	return nil
}

func expectMap(node *coreyaml.Node, label string) (map[string]*coreyaml.Node, error) {
	if node == nil || node.Kind != coreyaml.Map {
		return nil, fmt.Errorf("%s must be a map", label)
	}
	return node.Map, nil
}

func expectList(node *coreyaml.Node, label string) ([]*coreyaml.Node, error) {
	if node == nil || node.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s must be a list", label)
	}
	return node.List, nil
}

func rejectUnknownKeys(m map[string]*coreyaml.Node, allowed map[string]bool, label string) error {
	for key := range m {
		if allowed[key] || strings.HasPrefix(key, "x_") || strings.HasPrefix(key, "x-") {
			continue
		}
		return fmt.Errorf("unknown key %q in %s", key, label)
	}
	return nil
}

func optionalStringValue(node *coreyaml.Node, label string) (string, error) {
	if node == nil {
		return "", nil
	}
	if node.Kind != coreyaml.Scalar {
		return "", fmt.Errorf("%s must be a string", label)
	}
	value, ok := node.Value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", label)
	}
	return value, nil
}

func requiredBoolValue(node *coreyaml.Node, label string) (bool, error) {
	if node == nil || node.Kind != coreyaml.Scalar {
		return false, fmt.Errorf("%s must be a boolean", label)
	}
	v, ok := node.Value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", label)
	}
	return v, nil
}

func optionalIntValue(node *coreyaml.Node, label string) (int, error) {
	if node == nil {
		return 0, nil
	}
	if node.Kind != coreyaml.Scalar {
		return 0, fmt.Errorf("%s must be an integer", label)
	}
	switch v := node.Value.(type) {
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", label)
	}
}

func requiredStringListValue(node *coreyaml.Node, label string) ([]string, error) {
	if node == nil || node.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s must be a list of strings", label)
	}
	out := make([]string, 0, len(node.List))
	for i, item := range node.List {
		if item.Kind != coreyaml.Scalar {
			return nil, fmt.Errorf("%s[%d] must be a string", label, i)
		}
		value, ok := item.Value.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", label, i)
		}
		out = append(out, value)
	}
	return out, nil
}

func mapValue(node *coreyaml.Node) map[string]any {
	if node == nil || node.Kind != coreyaml.Map {
		return nil
	}
	out := map[string]any{}
	for key, child := range node.Map {
		out[key] = anyValue(child)
	}
	return out
}

func anyValue(node *coreyaml.Node) any {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case coreyaml.Scalar:
		return node.Value
	case coreyaml.List:
		out := make([]any, 0, len(node.List))
		for _, item := range node.List {
			out = append(out, anyValue(item))
		}
		return out
	case coreyaml.Map:
		return mapValue(node)
	default:
		return nil
	}
}

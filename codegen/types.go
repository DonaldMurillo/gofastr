package codegen

import "context"

// ProtocolVersion is the current external extension protocol version.
const ProtocolVersion = 1

// Config is the top-level YAML code generation configuration.
type Config struct {
	Version int           `json:"version"`
	Codegen CodegenConfig `json:"codegen"`
}

// CodegenConfig configures one generator run.
type CodegenConfig struct {
	Output     string            `json:"output"`
	Clean      *bool             `json:"clean,omitempty"`
	Generators []GeneratorConfig `json:"generators,omitempty"`
	Extensions []ExtensionConfig `json:"extensions,omitempty"`
}

// GeneratorConfig configures one generator invocation.
type GeneratorConfig struct {
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name"`
	Extension string         `json:"extension,omitempty"`
	Source    SourceConfig   `json:"source,omitempty"`
	Output    string         `json:"output,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
}

// SourceConfig describes structured input consumed by a generator.
type SourceConfig struct {
	Type   string         `json:"type,omitempty"`
	Path   string         `json:"path,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}

// ExtensionConfig describes an extension process or in-process extension.
type ExtensionConfig struct {
	Name    string         `json:"name"`
	Command []string       `json:"command,omitempty"`
	Config  map[string]any `json:"config,omitempty"`
}

// Diagnostic is a machine-readable message produced during generation.
type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// GeneratedFile is a pending file produced by a generator or extension.
type GeneratedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Owner   string `json:"owner,omitempty"`
}

// JSONDocument is one JSON file loaded by a json_dir source.
type JSONDocument struct {
	Path string `json:"path"`
	Data any    `json:"data"`
}

// Context is the shared generation context passed to generators/extensions.
type Context struct {
	ProjectDir  string
	Config      Config
	Metadata    map[string]any
	Inputs      map[string]any
	Files       *FileSet
	Diagnostics []Diagnostic
}

// Generator emits files for one configured generator entry.
type Generator interface {
	Name() string
	Generate(ctx context.Context, genCtx *Context, cfg GeneratorConfig) ([]GeneratedFile, error)
}

// Extension participates in code generation through named phases.
type Extension interface {
	Name() string
	RunPhase(ctx context.Context, phase string, genCtx *Context, gen GeneratorConfig, ext ExtensionConfig) (ExtensionResponse, error)
}

// ExtensionRequest is the JSON payload sent to external command extensions.
type ExtensionRequest struct {
	ProtocolVersion int             `json:"protocol_version"`
	Phase           string          `json:"phase"`
	ProjectDir      string          `json:"project_dir"`
	Generator       GeneratorConfig `json:"generator"`
	Extension       ExtensionConfig `json:"extension"`
	Source          any             `json:"source,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
	Files           []GeneratedFile `json:"files,omitempty"`
}

// ExtensionResponse is the JSON payload returned by extensions.
type ExtensionResponse struct {
	ProtocolVersion int             `json:"protocol_version,omitempty"`
	Diagnostics     []Diagnostic    `json:"diagnostics,omitempty"`
	Files           []GeneratedFile `json:"files,omitempty"`
	Deletes         []string        `json:"deletes,omitempty"`
}

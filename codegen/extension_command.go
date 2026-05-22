package codegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type commandExtension struct {
	name    string
	command []string
	stderr  io.Writer
}

// NewCommandExtension creates an Extension backed by an external command.
func NewCommandExtension(name string, command []string, stderrWriter io.Writer) Extension {
	return &commandExtension{name: name, command: command, stderr: stderrWriter}
}

func (e *commandExtension) Name() string { return e.name }

func (e *commandExtension) RunPhase(ctx context.Context, phase string, genCtx *Context, gen GeneratorConfig, ext ExtensionConfig) (ExtensionResponse, error) {
	if len(e.command) == 0 {
		return ExtensionResponse{}, fmt.Errorf("extension command is empty")
	}
	req := ExtensionRequest{
		ProtocolVersion: ProtocolVersion,
		Phase:           phase,
		ProjectDir:      genCtx.ProjectDir,
		Generator:       gen,
		Extension:       ext,
		Source:          genCtx.Inputs[generatorInputKey(gen, 0)],
		Metadata:        genCtx.Metadata,
		Files:           genCtx.Files.All(),
	}
	body, err := json.Marshal(req)
	if err != nil {
		return ExtensionResponse{}, err
	}
	command := e.command[0]
	cmd := exec.CommandContext(ctx, command, e.command[1:]...)
	cmd.Dir = genCtx.ProjectDir
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if e.stderr != nil && stderr.Len() > 0 {
			_, _ = e.stderr.Write(stderr.Bytes())
		}
		return ExtensionResponse{}, err
	}
	if e.stderr != nil && stderr.Len() > 0 {
		_, _ = e.stderr.Write(stderr.Bytes())
	}
	if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
		return ExtensionResponse{}, nil
	}
	var res ExtensionResponse
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&res); err != nil {
		return ExtensionResponse{}, fmt.Errorf("decode extension response: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ExtensionResponse{}, fmt.Errorf("decode extension response: trailing JSON content")
		}
		return ExtensionResponse{}, fmt.Errorf("decode extension response: trailing JSON content: %w", err)
	}
	if res.ProtocolVersion != 0 && res.ProtocolVersion != ProtocolVersion {
		return ExtensionResponse{}, fmt.Errorf("extension response protocol_version %d is not supported", res.ProtocolVersion)
	}
	return res, nil
}

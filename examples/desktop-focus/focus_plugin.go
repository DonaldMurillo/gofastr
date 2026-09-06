package main

import (
	"context"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
)

// focusPlugin registers the `focus` capability: the ONLY way the page
// talks to the engine. start/pause/resume/skip mutate, state reads;
// each mutation returns the resulting State so the caller can sync
// immediately instead of waiting for the next focus_tick. Ungated: the
// capability drives the owner's own timer rows and nothing else.
type focusPlugin struct {
	eng *Engine
}

func (focusPlugin) Name() string { return "focus" }

func (p focusPlugin) Init(app *framework.App) error {
	d, err := desktop.FromApp(app)
	if err != nil {
		// No desktop battery mounted: the plugin is a no-op, not a
		// startup failure (the --serve shape still runs the screens).
		app.Logger().Warn("desktop-focus: focus plugin found no desktop battery", "error", err)
		return nil
	}
	const stateSchema = `{"type":"object","properties":{` +
		`"phase":{"type":"string","enum":["idle","work","break","paused"]},` +
		`"remaining":{"type":"integer"},"taskId":{"type":"string"},` +
		`"taskTitle":{"type":"string"},"sessionId":{"type":"string"},` +
		`"endsAt":{"type":"string"}}}`
	startInput := json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"}},"additionalProperties":false}`)
	// mutating wraps an engine mutation so every answer carries the
	// new State.
	mutating := func(run func(context.Context) error) func(context.Context, json.RawMessage) (any, error) {
		return func(ctx context.Context, _ json.RawMessage) (any, error) {
			if err := run(ctx); err != nil {
				return nil, err
			}
			return map[string]any{"state": p.eng.State(ctx)}, nil
		}
	}
	return d.Register(desktop.Capability{
		Name:        "focus",
		Description: "The Focus timer engine: start, pause, resume, skip, and read the current state.",
		Version:     1,
		Methods: []desktop.Method{
			{
				Name:        "start",
				Description: "Starts a work session, optionally on a task.",
				Input:       startInput,
				Output:      json.RawMessage(stateSchema),
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					id, err := taskIDFrom(in)
					if err != nil {
						return nil, err
					}
					if err := p.eng.Start(ctx, id); err != nil {
						return nil, err
					}
					return map[string]any{"state": p.eng.State(ctx)}, nil
				},
			},
			{
				Name:        "pause",
				Description: "Pauses the running session.",
				Output:      json.RawMessage(stateSchema),
				Handler:     mutating(p.eng.Pause),
			},
			{
				Name:        "resume",
				Description: "Resumes the paused session.",
				Output:      json.RawMessage(stateSchema),
				Handler:     mutating(p.eng.Resume),
			},
			{
				Name:        "skip",
				Description: "Ends the current session now and moves to the next phase.",
				Output:      json.RawMessage(stateSchema),
				Handler:     mutating(p.eng.Skip),
			},
			{
				Name:        "state",
				Description: "Returns the timer's current state.",
				Output:      json.RawMessage(stateSchema),
				Handler: func(ctx context.Context, _ json.RawMessage) (any, error) {
					return map[string]any{"state": p.eng.State(ctx)}, nil
				},
			},
		},
	})
}

// taskIDFrom decodes the start input's optional taskId. A malformed
// body is invalid_input, not an internal error.
func taskIDFrom(in json.RawMessage) (string, error) {
	if len(in) == 0 {
		return "", nil
	}
	var body struct {
		TaskID string `json:"taskId"`
	}
	if err := handler.UnmarshalStrict(in, &body); err != nil {
		return "", &desktop.Error{Code: desktop.CodeInvalidInput, Message: "body must be an object with an optional taskId"}
	}
	return body.TaskID, nil
}

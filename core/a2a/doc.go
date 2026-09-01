// Package a2a implements the server side of the Agent2Agent protocol
// v1.0 task exchange over its JSON-RPC binding: SendMessage,
// SendStreamingMessage, GetTask, ListTasks, CancelTask, SubscribeToTask,
// the four push-notification-config operations, and GetExtendedAgentCard.
//
// Wire facts this package pins, each verified against the canonical
// a2a.proto and the A2A project's own Go SDK rather than recalled:
//
//   - JSON-RPC method names are the PascalCase RPC names ("SendMessage",
//     "GetTask", …). The v0.x slash forms ("message/send") are gone.
//   - Field names are camelCase; enums serialize as their proto names
//     ("TASK_STATE_COMPLETED", "ROLE_USER"); timestamps are RFC 3339 UTC.
//   - A Part is a flat object carrying exactly one of text / raw / url /
//     data plus optional filename, mediaType, metadata.
//   - A streaming result is a StreamResponse: an object with exactly one of
//     task / message / statusUpdate / artifactUpdate.
//   - Streaming methods answer with text/event-stream; each event's data is
//     a complete JSON-RPC response object whose result is a StreamResponse.
//     The stream closes once the task is terminal or interrupted.
//   - Push notifications POST a StreamResponse to the configured URL with
//     the A2A-Notification-Token header carrying the config's token.
//
// The agent card (discovery) lives in framework/uihost; this package is
// the exchange behind the card's JSONRPC interface. Skills are what the
// server does with a message: a GoFastr app is a deterministic agent, so
// a skill is invoked by name (Message.metadata["skill"], or a data part
// carrying a "skill" key), never inferred from prose. See framework/docs/
// content/a2a.md.
package a2a

// Package chat installs the in-app Kiln chat panel. The panel is a small
// vanilla-JS UI mounted under /kiln/* on Live's auxiliary router so it
// survives world rebuilds. It speaks to the host via:
//
//   GET  /kiln/chat              the panel HTML
//   GET  /kiln/chat/panel.js     the panel runtime
//   GET  /kiln/chat/panel.css    the panel stylesheet
//   GET  /kiln/world             current Session as JSON
//   POST /kiln/chat/message      shortcut for tools.Chat
//   POST /kiln/tool/{name}       generic tool dispatch
//   GET  /.kiln/events           live SSE stream of session events
//
// The panel itself does not need world IR coverage — it's the host's
// own surface, not something the agent edits. Kiln-built apps still
// drive the panel by issuing tool calls; the panel is just the eyes.
package chat

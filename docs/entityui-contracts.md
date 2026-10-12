# Entity screen routes and writes

App-owned entity screens use a shared `*entityui.UI` from `App.EntityUI`.
The app registers the screen routes; the builders render their contents.

`List("deals").Base("/deals")` defaults to record links at `/deals/{id}`
and create links at `/deals/create`. Mount `Create("deals").Base("/deals")`
at that exact create path, or configure `UI.WithCreatePath` with the same
function used to register a custom create route such as `/deals/new`.
Use the returned UI throughout the app. The callback returns the complete
path and a boolean; false or an empty path hides create links for that
entity. New, Duplicate, Create another, related-list Add and picker New
all share the setting, retaining their existing access checks and query
parameters. The path has no query or fragment. A nil callback keeps the
UI unchanged, and the default remains `<base>/create`.

Entityui state-move buttons post to the entity's transition route through
the RPC runtime. That runtime sends `Content-Type: application/json` on
bodyless writes, satisfying the transition route's existing JSON gate.
Bodyless GET and HEAD requests carry no content type; multipart forms
retain the browser-generated boundary.

The embedded [entityui guide](../framework/docs/content/entityui.md) has
the routing example. See also the [state route contract](../framework/docs/content/states.md)
and [RPC runtime contract](../framework/docs/content/runtime-contract.md).

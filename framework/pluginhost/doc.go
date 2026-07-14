// Package pluginhost is the reusable, plugin-agnostic host glue for GoFastr
// heavy-JS plugins that run inside an opaque-origin sandboxed iframe.
//
// It distils the platform machinery out of the wysiwyg plugin (the first such
// plugin) so a second heavy-JS plugin can reuse it instead of reimplementing
// the iframe / broker / manifest / capability / framing-header plumbing. See
// ../docs/design/protocol-v1.md for the authoritative protocol contract this
// package implements.
//
// What lives here (the platform's job):
//
//   - [Manifest] / [ClientModule]: the declarative description of a plugin's
//     client module (entry document, sandbox policy, capabilities, schema).
//   - [AssetServer]: serves a plugin's embedded client assets with the correct
//     Content-Types AND the framing/CORP/CSP header relaxation GoFastr's global
//     security middleware otherwise blocks (the client-side isolation contract).
//   - [Allow]: the capability gate reusing battery/auth's resource:verb scopes.
//   - [MountMarker]: the generic mount-marker + hidden-field HTML the generic
//     broker scans for.
//   - [BrokerScriptURL] / [RegisterBrokerRoute] / [UIHostOption]: serving and
//     injection of the generic host broker (host/pluginhost.js).
//
// A plugin (wysiwyg, and future plugins from Worker G) composes these pieces in
// its Init and ships a thin JS adapter that registers its plugin-specific event
// handlers with the generic broker (see [BrokerRegistration]).
package pluginhost

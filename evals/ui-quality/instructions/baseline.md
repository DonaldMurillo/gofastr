# GoFastr application instructions — baseline

Build the requested application as a complete, runnable GoFastr app.

- SSR-first. Use normal links for page navigation and server-driven islands for
  in-page state. Do not add feature-specific JavaScript.
- Survey `framework/ui`, `core-ui/app`, and `core-ui/patterns` before building.
- Use existing GoFastr components, layouts, theme tokens, and registered styles.
- Do not recreate component internals or interaction behavior.
- Applications ship zero bespoke CSS and zero hand-rolled structural markup.
  Add a missing reusable capability upstream in the design system.
- Use realistic content. Implement every route named in `EVAL_TASK.md`.
- Make the app responsive and support light and dark themes.
- Run `go test ./...` before declaring completion.

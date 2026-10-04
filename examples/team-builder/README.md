# team-builder

A Pokémon team builder whose team lives in the visitor's browser. It is
the example for `core-ui/localdb` and `framework/localentity`.

```bash
go run ./examples/team-builder
# open http://localhost:8093
```

- `team.go` declares the database and the `members` entity: three
  `core/schema` fields, ordered by level, at most six records.
- `screens.go` renders the page from `framework/ui` only: a `ui.Form`
  wrapped by `Members.Form`, and a `ui.Grid` of `ui.Card`s from
  `Members.List`.
- There is no database, no handler, and no JavaScript in the app. The
  `localentity` behaviour saves the form into IndexedDB and renders the
  team from it.

Open two tabs: a save, an edit or a release in one shows up in the
other. Reload: the team is still there. Clear site data: it is gone.

`browser_test.go` drives all of that in Chromium. Set
`TEAM_BUILDER_SHOTS=<dir>` to also write screenshots (light, dark,
desktop, phone).

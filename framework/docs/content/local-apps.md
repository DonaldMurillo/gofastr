# Local apps: one person, one device

Some apps run for one person on their own device and have no sign-in
of their own: a desktop window (`battery/desktop`), a background
service the user runs (`battery/service`), or a phone app that runs its
GoFastr server inside the app process. Each one keeps the same three
things in a private data directory. `framework/localapp` opens that
directory, so every host gets the same file modes and the same
first-run behaviour.

## Open a data directory

<!-- gofastr:compile
import "log"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/localapp"
var dataDir string
-->
```go
data, err := localapp.Open(dataDir)
if err != nil {
	log.Fatal(err)
}
app := framework.NewApp(
	framework.WithDB(data.DB),
	framework.WithSecret(data.Secret),
)
_ = app
```

`dataDir` must be an absolute path that the platform gives the app,
such as Application Support on macOS or an Android app's files
directory. Open creates it 0700 when it is missing and leaves the mode
of an existing directory alone. It holds:

| File | What it is |
|---|---|
| `app.db` | The SQLite database (`data.DB`), created 0600 before SQLite opens it. `localapp` registers the `sqlite3` driver itself, and any directory name works (a `#`, `?` or `%` in it is escaped). |
| `secret` | The session secret (`data.Secret`), 32 bytes of `crypto/rand` as unpadded base64url, 0600. Sessions stay valid across restarts. A file in any other shape is refused. |
| `identity` | The installation's owner id (`data.User`), 16 bytes of `crypto/rand` as hex, 0600. |

On first run Open mints the secret and the identity with an exclusive
create, so two processes racing the first boot read the same values.
A damaged file is refused, never replaced. A new secret would sign
every session out, and a new identity would hide every row the old
one owns. To start over, delete the file.

`localapp.OpenDB(dir)` and `localapp.LoadOrMintSecret(dir)` open just
one piece, for a host that resolves the directory itself (the desktop
battery's `AppOptions`).

## The local identity

Owner-scoped CRUD (`EntityConfig.Scope.OwnerField`) needs an owner id
even when nobody signs in. The host puts `data.User` into each request
it serves with `handler.SetUser`. `localapp.Install()` sets the owner
extractor that reads it back:

- Install sets the extractor only when none is set, and returns
  `true` when another one (battery/auth's, set from its package init)
  owns identity.
- When Install returns `true`, the host must not hand out the local
  identity at all. battery/auth marks a signed-out request with a nil
  user, and `handler.GetUser` cannot tell that apart from no decision,
  so a fallback would turn every signed-out visitor into the owner.
- `data.User.GetRoles()` is `["admin"]`: the one person who can reach
  the app owns it. The host decides which requests come from that
  person. The desktop battery hands the identity only to requests
  from its own window. An in-process mobile server has no socket, so
  every request comes from the app's own WebView.

`localapp.Valid(id)` checks the id grammar, and `localapp.ForID(id)`
builds a `User` for a test.

## Common mistakes

- **Handing out the local identity when `Install` returned `true`.**
  battery/auth owns identity in that binary. Serve the request as it
  arrives.
- **Passing a relative or working-directory path.** Open refuses it.
  A desktop app launched from Finder and a phone app have no useful
  working directory.
- **Deleting `identity` to fix an error.** That mints a new owner id
  and every stored row stays with the old one. Restore the file when
  you can.

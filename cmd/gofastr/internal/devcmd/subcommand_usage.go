package devcmd

import (
	"fmt"
)

func printDevUsage() {
	fmt.Println(`Usage: gofastr dev [--addr=<host:port>] [--dir=<path>] [--pkg=<path>] [--no-a11y] [-p <port>]

Builds and runs the server, rebuilding and hot-reloading on file changes.
GOFASTR_DEV=1 is injected so the app wires livereload.

--addr=<host:port>  listen address (default localhost:8080); -p <port> is a short form
--dir=<path>        project root: watch root and server cwd (default .)
--pkg=<path>        package to build, relative to --dir (default .)
--no-a11y           skip the accessibility gate (.ui.go sandbox gate still runs)`)
}

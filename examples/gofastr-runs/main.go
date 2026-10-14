// Command gofastr-runs lists every gofastr app running on this
// machine, from the local run records each App writes when its port
// opens (framework/localrun), and serves that list to agents over MCP.
// It is the service battery's example: it installs as a per-user OS
// service, keeps running after the terminal closes, and is used from
// the browser or from an agent.
//
//	go run ./examples/gofastr-runs                     # foreground dev run
//	go run ./examples/gofastr-runs service run --open  # same, browser signed in
//	go build -o gofastr-runs ./examples/gofastr-runs
//	./gofastr-runs service install                     # copy, register, start
//	./gofastr-runs service open                        # sign in from the browser
//	./gofastr-runs service mcp                         # stdio MCP, for an agent's config
//	./gofastr-runs service status | logs | stop | uninstall
//	gofastr dev --dir examples/gofastr-runs            # the dev loop
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/service"
)

// appID names the launchd label, the systemd unit, the Windows task,
// and the state directory.
const appID = "dev.gofastr.runs"

// port is the installed daemon's fixed loopback port, so a bookmark
// survives restarts. `gofastr-runs service install --port N` overrides it.
const port = 47811

func main() {
	svc := service.New(service.Config{ID: appID, Name: "gofastr-runs", Port: port})
	if err := svc.Run(context.Background(), os.Args[1:], buildApp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

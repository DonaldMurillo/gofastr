// Command service-hello is the service battery's example: an app that
// installs as a per-user OS service, keeps running after the terminal
// closes, and is used from the browser.
//
//	go run ./examples/service-hello                     # foreground dev run
//	go run ./examples/service-hello service run --open  # same, browser signed in
//	go build -o hello ./examples/service-hello
//	./hello service install                             # copy, register, start
//	./hello service open                                # sign in from the browser
//	./hello service status | logs | stop | uninstall
//	gofastr dev --dir examples/service-hello            # the dev loop
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/service"
)

// appID names the launchd label, the systemd unit, the Windows task,
// and the state directory.
const appID = "dev.gofastr.service-hello"

// port is the installed daemon's fixed loopback port, so a bookmark
// survives restarts. `hello service install --port N` overrides it.
const port = 47811

func main() {
	svc := service.New(service.Config{ID: appID, Name: "hello", Port: port})
	if err := svc.Run(context.Background(), os.Args[1:], buildApp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

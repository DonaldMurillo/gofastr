// Command launchmain prints desktop.LaunchArgs(os.Args[1:]) as JSON, so
// a desktopcmd test can prove the --launch-args stamp reaches it.
package main

import (
	"encoding/json"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func main() {
	_ = json.NewEncoder(os.Stdout).Encode(desktop.LaunchArgs(os.Args[1:]))
}

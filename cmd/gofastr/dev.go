package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func runDev(args []string) {
	addr := "localhost:8080"
	for i, a := range args {
		if a == "--addr" && i+1 < len(args) {
			addr = args[i+1]
		}
		if a == "-p" && i+1 < len(args) {
			addr = "localhost:" + args[i+1]
		}
	}

	fmt.Printf("\n  %s Dev server starting...\n\n", bold("GoFastr"))
	info("Watching for file changes...")
	info("Server at http://%s", addr)
	fmt.Println()

	for {
		info("Building and starting server...")
		cmd := exec.Command("go", "run", ".", "--addr", addr)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(), "PORT="+addr)

		err := cmd.Run()
		if err != nil {
			fail("Server exited: %v", err)
		}

		fmt.Println()
		info("Restarting in 2 seconds... (press Ctrl+C to stop)")
		time.Sleep(2 * time.Second)
	}
}

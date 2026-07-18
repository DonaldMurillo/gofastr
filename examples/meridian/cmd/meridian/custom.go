package main

import (
	client "github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
)

// This file is yours — `gofastr generate cli --force` never overwrites it.

// customCommands is merged OVER the generated command set: an entry whose
// name matches a generated command ("plans list") replaces it, and new
// names become new commands. To wrap rather than replace a generated verb,
// call its run function (runPostsList-style) from your own.
func customCommands() []command {
	return nil
}

// configureClient runs on every request-bearing command right after the
// client is built — set default headers via a custom c.HTTP transport,
// tweak timeouts, or point at a mock during tests.
func configureClient(c *client.Client) {}

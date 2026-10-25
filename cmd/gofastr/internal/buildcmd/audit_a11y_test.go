package buildcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestBuildA11yGateBlocksOnViolations(t *testing.T) {
	root := clitest.WriteA11yTree(t, map[string]string{
		"app/screen.go": clitest.BadScreen,
	})
	if buildA11yGate(root) {
		t.Errorf("gate must block a tree with violations")
	}
}

func TestBuildA11yGatePassesCleanTree(t *testing.T) {
	root := clitest.WriteA11yTree(t, map[string]string{
		"app/screen.go": `package app

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func view() any {
	return html.Image(html.ImageConfig{Src: "/x.png", Alt: "Launch team photo"})
}
`,
	})
	if !buildA11yGate(root) {
		t.Errorf("gate must pass a clean tree")
	}
}

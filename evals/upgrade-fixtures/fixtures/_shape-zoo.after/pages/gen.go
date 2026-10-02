//go:build ignore

// gen.go is a `go run gen.go` generator: package main in another
// package's directory, built only when named on the command line.
package main

import "fmt"

func main() { fmt.Println("<header>Zoo</header>") }

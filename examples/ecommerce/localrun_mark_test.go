package ecommerce_test

// The flagship tests build the generated app and run it as a child.
// This package imports nothing that links framework/localrun, whose
// init marks a test binary's environment, so without this import the
// child registered in the real run registry and, killed at cleanup,
// left its record behind.
import _ "github.com/DonaldMurillo/gofastr/framework/localrun"

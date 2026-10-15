package upgradefixtures

// The fixtures build each upgraded app and run it as a child. This
// package imports nothing that links framework/localrun, whose init
// marks a test binary's environment, so without this import the child
// registered in the real run registry.
import _ "github.com/DonaldMurillo/gofastr/framework/localrun"

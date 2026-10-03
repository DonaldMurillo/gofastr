package pages

import "github.com/DonaldMurillo/gofastr/core-ui/html"

// The key hit lands here, the line to edit, not at the ExtraAttrs use in
// button.go. Attribute names compare with ASCII case folded.
var saveAttrs = html.Attrs{"Disabled": ""} // zoo:hit disabled

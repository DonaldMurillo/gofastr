package framework

// The sandbox conformance suite (P1–P7, processmodule_probe.go) needs a
// child that attempts each forbidden action under the backend and prints
// what happened. That child is the RUNNING binary, re-executed through
// os.Executable with GOFASTR_SANDBOX_PROBE=<id> in its environment: the
// test binary under `go test`, the host app's own binary in production
// (App.ProcessModuleSandbox, the first untrusted RegisterProcessModule).
//
// The dispatch lives in this package's init, not in a hook the host calls
// from main. Issue #608 weighed two shapes for the probe child:
//
//   - The host binary, with an exported ProbeChildMaybeRun() every host
//     calls first thing in main. Forget the call and the probe launches a
//     second copy of the app instead of the probe body.
//   - A dedicated helper binary that ships beside the app. gofastr builds
//     it, every deploy has to carry it, and the probe then proves the
//     helper's confinement rather than anything the host ran.
//
// An init in package framework keeps the first shape without its footgun:
// only code in this package launches a probe child, so any binary that
// can launch one has linked this package, and this init has run before
// main. Nothing to forget, nothing extra to ship.
//
// What it costs: the probe child runs every package init that sorts ahead
// of this package's, the host's own included, before it exits here. They
// run inside the sandbox under test with a scrubbed environment, so an
// init that needs the network or a secret fails in the probe child the
// way it would in a confined module. GOFASTR_SANDBOX_PROBE is reserved:
// a process started with it set runs one probe and exits.
func init() {
	probeChildMaybeRun()
}

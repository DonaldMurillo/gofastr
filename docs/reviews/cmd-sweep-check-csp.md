## check-csp and vettool

cmd/check-csp/main.go: walk error exits 1, not 2 -> TestCLIExitsTwoWhenTheWalkFails
cmd/check-csp/main.go: inline-script result ignored -> TestCLINormalizesExplicitRootSpelling
cmd/check-csp/main.go: inline-style result ignored -> TestCLINormalizesExplicitRootSpelling
cmd/check-csp/main.go: never exits non-zero on violations -> TestCLINormalizesExplicitRootSpelling

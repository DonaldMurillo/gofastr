package auditcmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/gates"
)

// runAuditA11y is the `gofastr audit a11y` entry point.
//
//	gofastr audit a11y [root]              static lint (default root ".")
//	gofastr audit a11y --url <base>        axe-core scan of a running app
//	    [--email EMAIL]                    sign in through /login first
//	    [--password -]                     password: "-" reads stdin;
//	                                       GOFASTR_AUDIT_PASSWORD env wins
//	    [--pages /a,/b]                    explicit page list (default: the
//	                                       app's /sitemap.xml, else "/")
//
// Both modes exit 1 when issues are found, so either can gate CI.
func runAuditA11y(args []string) {
	opts := parseA11yArgs(args)
	if opts.help {
		fmt.Println("Usage: gofastr audit a11y [root] [--url <base>] [--email EMAIL --password -] [--pages /a,/b]")
		fmt.Println("  --password - reads the secret from stdin; GOFASTR_AUDIT_PASSWORD env also works (preferred: argv is ps-visible)")
		fmt.Println()
		fmt.Println("Static mode (default): lints every .go file under root for missing")
		fmt.Println("required accessibility fields on core-ui/html elements (Alt on")
		fmt.Println("images, Label on buttons/landmarks, For on labels, …) and explains")
		fmt.Println("how to fix each finding. The same check runs in `gofastr build`.")
		fmt.Println()
		fmt.Println("Runtime mode (--url): runs the vendored axe-core engine via headless")
		fmt.Println("Chrome against the running app, checking contrast, focus, landmarks, and ARIA")
		fmt.Println("under BOTH color schemes. Pages come from the app's /sitemap.xml")
		fmt.Println("(uihost.WithSitemap) unless --pages is given. Pass --email and")
		fmt.Println("--password - (stdin; or GOFASTR_AUDIT_PASSWORD) to submit the app's")
		fmt.Println("/login form before discovery/auditing.")
		cli.OsExit(0)
	}
	if opts.badFlag != "" {
		fmt.Fprintf(os.Stderr, "audit a11y: unknown flag %s\n", opts.badFlag)
		cli.OsExit(2)
	}
	if (opts.email == "") != (opts.password == "") {
		fmt.Fprintln(os.Stderr, "audit a11y: --email and --password must be provided together")
		cli.OsExit(2)
	}

	if opts.baseURL != "" {
		run, err := auditA11yURL(opts.baseURL, opts.pages, a11yCredentials{
			Email: opts.email, Password: opts.password,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "audit a11y: %v\n", err)
			cli.OsExit(1)
		}
		fmt.Print(formatAxeReport(run))
		if run.Incomplete() {
			cli.OsExit(1)
		}
		for _, r := range run.Results {
			if len(r.Violations) > 0 {
				cli.OsExit(1)
			}
		}
		return
	}

	findings, err := gates.AuditA11y(opts.root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit a11y: %v\n", err)
		cli.OsExit(1)
	}
	fmt.Print(gates.FormatA11yReport(findings))
	if len(findings) > 0 {
		cli.OsExit(1)
	}
}

// parseA11yArgs resolves the `audit a11y` argument forms: an optional
// positional root, `--url`/`--pages` in BOTH `--flag=value` and
// `--flag value` spellings (the docs use the space form), and
// --help/-h. badFlag carries the first unrecognized flag, "" when all
// args parsed.
type a11yArgs struct {
	root, baseURL, email, password string
	pages                          []string
	help                           bool
	badFlag                        string
}

func parseA11yArgs(args []string) a11yArgs {
	out := a11yArgs{root: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "--url" || arg == "--pages" || arg == "--email" || arg == "--password") && i+1 < len(args) {
			arg = arg + "=" + args[i+1]
			i++
		}
		switch {
		case arg == "--help" || arg == "-h":
			out.help = true
		case strings.HasPrefix(arg, "--url="):
			out.baseURL = strings.TrimPrefix(arg, "--url=")
		case strings.HasPrefix(arg, "--email="):
			out.email = strings.TrimPrefix(arg, "--email=")
		case strings.HasPrefix(arg, "--password="):
			out.password = resolveAuditPassword(strings.TrimPrefix(arg, "--password="))
		case strings.HasPrefix(arg, "--pages="):
			for _, p := range strings.Split(strings.TrimPrefix(arg, "--pages="), ",") {
				if p = strings.TrimSpace(p); p != "" {
					out.pages = append(out.pages, p)
				}
			}
		case !strings.HasPrefix(arg, "-"):
			out.root = arg
		default:
			if out.badFlag == "" {
				out.badFlag = arg
			}
		}
	}
	return out
}

// resolveAuditPassword implements the credentials-never-in-argv doctrine
// for `audit a11y --password`. "-" is the conventional stdin marker:
// resolution order is GOFASTR_AUDIT_PASSWORD first, then piped stdin,
// and the literal marker is never used as the password. A literal value
// keeps working (documented interface) but draws a one-line warning: it
// sits in ps/procfs world-readable state for the whole scan, the exact
// local observer the marker channels exist to avoid.
func resolveAuditPassword(literal string) string {
	if literal != "-" {
		fmt.Fprintln(os.Stderr, "warning: --password <value> is visible to every local process via ps; prefer GOFASTR_AUDIT_PASSWORD or `--password -` (reads the secret from stdin)")
		return literal
	}
	if env := os.Getenv("GOFASTR_AUDIT_PASSWORD"); env != "" {
		return env
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		fmt.Fprintln(os.Stderr, "warning: --password - read an empty secret from stdin")
	}
	return secret
}

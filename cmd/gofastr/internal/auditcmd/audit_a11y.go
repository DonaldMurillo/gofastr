package auditcmd

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/gates"
)

func runAuditA11y(args []string) {
	opts := parseA11yArgs(args)
	if opts.help {
		fmt.Println("Usage: gofastr audit a11y [root] [--url <base>] [--email EMAIL --password -] [--pages /a,/b]")
		fmt.Println("  --password - reads the secret from stdin; GOFASTR_AUDIT_PASSWORD env also works (preferred: argv is ps-visible)")
		fmt.Println()
		fmt.Println("Static mode (default): lints every non-test, non-generated .go file")
		fmt.Println("under root for missing accessibility fields on core-ui/html elements (Alt on")
		fmt.Println("images, Label on buttons/landmarks, For on labels, …) and explains")
		fmt.Println("how to fix each finding. The same check runs in `gofastr build`.")
		fmt.Println()
		fmt.Println("Runtime mode (--url): runs the vendored axe-core engine via headless")
		fmt.Println("Chrome against the running app, checking contrast, focus, landmarks, and ARIA")
		fmt.Println("under BOTH color schemes. Pages come from the app's /sitemap.xml")
		fmt.Println("(including split sitemaps) unless --pages is given. Pass --email and")
		fmt.Println("--password - (stdin; or GOFASTR_AUDIT_PASSWORD) to submit the app's")
		fmt.Println("/login form before discovery/auditing.")
		cli.OsExit(0)
	}
	if opts.badFlag != "" {
		cli.Fprintf(os.Stderr, "audit a11y: unknown flag %s\n", opts.badFlag)
		cli.OsExit(2)
	}
	if opts.missingValue != "" {
		cli.Fprintf(os.Stderr, "audit a11y: %v\n", cli.NeedsValue(opts.missingValue))
		cli.OsExit(2)
	}
	if (opts.email == "") != (opts.password == "") {
		fmt.Fprintln(os.Stderr, "audit a11y: --email and --password must be provided together")
		cli.OsExit(2)
	}

	if opts.baseURL != "" {
		base, err := url.Parse(opts.baseURL)
		if err != nil || base.Hostname() == "" || (base.Scheme != "http" && base.Scheme != "https") {
			fmt.Fprintln(os.Stderr, "audit a11y: --url must be an absolute HTTP or HTTPS URL")
			cli.OsExit(2)
		}
		var password string
		if opts.password != "" {
			password = resolveAuditPassword(opts.password)
			if password == "" {
				fmt.Fprintln(os.Stderr, "audit a11y: --password requires a non-empty secret")
				cli.OsExit(2)
			}
		}
		run, err := auditA11yURL(opts.baseURL, opts.pages, a11yCredentials{
			Email: opts.email, Password: password,
		})
		if err != nil {
			cli.Fprintf(os.Stderr, "audit a11y: %v\n", err)
			cli.OsExit(1)
		}
		cli.Print(formatAxeReport(run))
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
	if opts.email != "" {
		fmt.Fprintln(os.Stderr, "audit a11y: --email and --password require --url")
		cli.OsExit(2)
	}

	findings, err := gates.AuditA11y(opts.root)
	if err != nil {
		cli.Fprintf(os.Stderr, "audit a11y: %v\n", err)
		cli.OsExit(1)
	}
	cli.Print(gates.FormatA11yReport(findings))
	if len(findings) > 0 {
		cli.OsExit(1)
	}
}

type a11yArgs struct {
	root, baseURL, email, password string
	pages                          []string
	help                           bool
	badFlag                        string
	missingValue                   string
}

func parseA11yArgs(args []string) a11yArgs {
	out := a11yArgs{root: "."}
	firstErr := func() bool { return out.badFlag == "" && out.missingValue == "" }
	missing := func(flag string) {
		if firstErr() {
			out.missingValue = flag
		}
	}
	stringFlags := map[string]*string{"--url": &out.baseURL, "--email": &out.email, "--password": &out.password}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--url" || arg == "--pages" || arg == "--email" || arg == "--password" {
			if i+1 >= len(args) || cli.IsFlagToken(args[i+1]) {
				missing(arg)
				continue
			}
			arg = arg + "=" + args[i+1]
			i++
		}
		if name, val, ok := strings.Cut(arg, "="); ok {
			if dst := stringFlags[name]; dst != nil {
				if *dst = val; val == "" {
					missing(name)
				}
				continue
			}
		}
		switch {
		case arg == "--help" || arg == "-h":
			out.help = true
		case strings.HasPrefix(arg, "--pages="):
			n := len(out.pages)
			for _, p := range strings.Split(strings.TrimPrefix(arg, "--pages="), ",") {
				if p = strings.TrimSpace(p); p != "" {
					out.pages = append(out.pages, p)
				}
			}
			if len(out.pages) == n {
				missing("--pages")
			}
		case !strings.HasPrefix(arg, "-"):
			out.root = arg
		default:
			if firstErr() {
				out.badFlag = arg
			}
		}
	}
	return out
}

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

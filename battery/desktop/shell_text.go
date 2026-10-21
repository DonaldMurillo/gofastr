package desktop

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// ShellContext is ctx carrying the locale a shell's own words speak:
// the menus it adds, role items' default titles, the About box. It is
// the first of preferred (the OS's preferred languages, best first)
// the app's catalog has, else the one the environment names
// (i18n.EnvLocale), with the app's translator (i18n.Default) attached.
// A shell calls it once in Run and plans its menus under the result.
func ShellContext(ctx context.Context, preferred []string) context.Context {
	tr := i18n.Default()
	loc, ok := i18n.Match(tr, preferred...)
	if !ok {
		loc = i18n.EnvLocale(tr)
	}
	ctx = i18n.WithContext(ctx, loc)
	if tr != nil {
		ctx = i18n.WithTranslator(ctx, tr)
	}
	return ctx
}

// PromptWords is the permission prompt's text in one locale.
type PromptWords struct {
	// Message names the call and the permission it wants: the alert's
	// message on macOS.
	Message string
	// Title is the dialog's window title on Windows.
	Title string
	// Use asks to use the capability, the Windows instruction when the
	// method has no Description.
	Use string
	// Choose explains the three buttons; YesNo explains the Yes / No /
	// Cancel mapping of the Windows message-box fallback.
	Choose, YesNo string
	// The three buttons, in Allow, Allow once, Deny order.
	Allow, AllowOnce, Deny string
}

// PromptText is req's permission prompt in ctx's locale. Prompt's ctx
// is the bridge request's, so the prompt speaks the page's language.
func PromptText(ctx context.Context, req PermissionRequest) PromptWords {
	return PromptWords{
		Message: permissionAsks.Text(ctx, map[string]any{
			"capability": req.Capability, "method": req.Method, "permission": req.Permission,
		}),
		Title:     permissionTitle.Text(ctx),
		Use:       permissionUse.Text(ctx, map[string]any{"capability": req.Capability}),
		Choose:    permissionChoose.Text(ctx),
		YesNo:     permissionYesNo.Text(ctx),
		Allow:     permissionAllow.Text(ctx),
		AllowOnce: permissionOnce.Text(ctx),
		Deny:      permissionDeny.Text(ctx),
	}
}

// AboutText is the About box's title and body, for a shell that draws
// its own (Windows); macOS shows the system panel.
func AboutText(ctx context.Context) (title, body string) {
	return aboutTitle.Text(ctx), aboutBody.Text(ctx)
}

// AcceleratorNames are the modifier names a shell that spells
// accelerators out in menu rows (Windows) writes: Ctrl, Alt, Shift.
func AcceleratorNames(ctx context.Context) (ctrl, alt, shift string) {
	return acceleratorCtrl.Text(ctx), acceleratorAlt.Text(ctx), acceleratorShift.Text(ctx)
}

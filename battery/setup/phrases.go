package setup

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonSetupTokenInvalid  = i18n.Define("setup.token_invalid", "forbidden: invalid or expired setup token. Check the server startup log for the setup URL.")
	reasonSetupTokenRequired = i18n.Define("setup.token_required", "forbidden: setup token required. Check the server startup log for the setup URL.")
	reasonSetupTokenUsed     = i18n.Define("setup.token_used", "forbidden: setup token already used or expired. Continue in your original browser session, or restart the app to mint a fresh token.")
)

// The wizard's own words: its notices, buttons and the finished and
// unfinished pages.
var (
	pageTitle             = i18n.Define("setup.title", "Setup")
	pageStepFailed        = i18n.Define("setup.step_failed", "Step failed")
	pageForceTitle        = i18n.Define("setup.force_title", "Setup already completed")
	pageForceBody         = i18n.Define("setup.force_body", "You are in rescue mode (GOFASTR_SETUP=force). Re-running setup steps.")
	pageContinue          = i18n.Define("setup.continue", "Continue")
	pageCompleteSetup     = i18n.Define("setup.complete_setup", "Complete Setup")
	pageCompleteTitle     = i18n.Define("setup.complete_title", "Setup Complete")
	pageCompleteBody      = i18n.Define("setup.complete_body", "Your application is ready.")
	pageGoToApp           = i18n.Define("setup.go_to_app", "Go to the app →")
	pageNotCompleteTitle  = i18n.Define("setup.not_complete_title", "Setup is not complete")
	pageIncompleteHeading = i18n.Define("setup.incomplete_heading", "Setup incomplete")
	pageRetry             = i18n.Define("setup.retry", "Retry")
	pageIncompleteWiring  = i18n.Define("setup.incomplete_wiring", "Every step ran, but setup still reports incomplete. The Complete predicate does not observe the steps' writes, e.g. AdminStep configured with a different users table than the auth store writes to. Fix the wiring and restart.")
	pageCheckFailed       = i18n.Define("setup.check_failed", "Every step ran, but the completion check failed: {{cause}}. Fix the underlying issue and reload this page: setup finishes automatically once the check passes.")
)

// The built-in steps' titles, labels and refusals.
var (
	adminStepTitle      = i18n.Define("setup.admin_step", "Create Admin")
	adminEmailLabel     = i18n.Define("setup.admin_email", "Admin email")
	adminPasswordLabel  = i18n.Define("setup.admin_password", "Admin password")
	reasonEmailRequired = i18n.Define("setup.email_required", "email is required")
	healthStepTitle     = i18n.Define("setup.health_step", "Verify Adapters")
	reasonNotReady      = i18n.Define("setup.not_ready", "readiness checks failed:\n  - {{failures}}")
)

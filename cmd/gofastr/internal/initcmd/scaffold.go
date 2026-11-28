package initcmd

import _ "embed"

// The Go files init writes, as fmt.Sprintf formats where they take arguments.
var (
	//go:embed scaffold/main.go.tmpl
	mainGoTemplate string
	//go:embed scaffold/main_entities.go.tmpl
	mainEntitiesGoTemplate string
	//go:embed scaffold/screens.go.tmpl
	screensGoTemplate string
	//go:embed scaffold/screens_auth.go.tmpl
	authScreensGoTemplate string
	//go:embed scaffold/entities.go.tmpl
	entitiesGoTemplate string
)

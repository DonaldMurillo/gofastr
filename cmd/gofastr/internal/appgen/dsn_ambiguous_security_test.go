package appgen

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestDSNParseSuccessAmbiguousSecretsStayOutOfSource(t *testing.T) {
	for _, prefix := range []string{"123", "00080"} {
		for _, delimiter := range []string{"/", "?", "#"} {
			t.Run(prefix+delimiter, func(t *testing.T) {
				dsn := "postgres://u:" + prefix + delimiter + "credentialtail@host/db"
				if _, err := url.Parse(dsn); err != nil {
					t.Fatalf("fixture must parse successfully: %v", err)
				}
				spec := appspec.AppSpec{App: appspec.AppSpecApp{Name: "store", Module: "local/store", DBDriver: "postgres", DBURL: dsn}}
				for _, renderer := range []struct {
					name   string
					render func(appspec.AppSpec) string
				}{{name: "main", render: RenderAppSpecMain}, {name: "app", render: RenderAppSpecApp}} {
					t.Run(renderer.name, func(t *testing.T) {
						source := renderer.render(spec)
						if strings.Contains(source, "u:"+prefix) || strings.Contains(source, "credentialtail") {
							t.Error("generated source contains ambiguous DSN credentials")
						}
					})
				}
				if !strings.Contains(RenderAppSpecEnv(spec), dsn) {
					t.Error("original DSN missing from environment")
				}
			})
		}
	}
}

package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestDSNQuerySecretsStayOutOfSource(t *testing.T) {
	for _, dsn := range []string{
		"postgres://db/app?password=fixturecredential",
		"postgres://db/app?%70assword=fixturecredential",
		"host=db sslpassword=fixturecredential dbname=app",
	} {
		bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "store", Module: "local/store", DBDriver: "postgres", DBURL: dsn}}
		for _, source := range []string{RenderAppSpecMain(bp), RenderAppSpecApp(bp)} {
			if strings.Contains(source, "fixturecredential") {
				t.Errorf("generated source contains DSN credential for %s", dsn)
			}
		}
		if !strings.Contains(RenderAppSpecEnv(bp), dsn) {
			t.Errorf("credential missing from environment for %s", dsn)
		}
	}
}

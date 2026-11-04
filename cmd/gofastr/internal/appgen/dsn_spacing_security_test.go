package appgen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestSpacedDSNSecretsStayInEnv(t *testing.T) {
	for _, key := range []string{"password", "sslpassword"} {
		dsn := fmt.Sprintf("host=db %s = fixturecredential dbname=store", key)
		bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "store", Module: "local/store", DBDriver: "postgres", DBURL: dsn}}
		for _, source := range []string{RenderAppSpecMain(bp), RenderAppSpecApp(bp)} {
			if strings.Contains(source, "fixturecredential") {
				t.Errorf("generated source contains spaced %s", key)
			}
		}
		if !strings.Contains(RenderAppSpecEnv(bp), dsn) {
			t.Errorf("spaced %s missing from environment", key)
		}
	}
}

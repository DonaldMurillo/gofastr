package crud

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/filter"
)

func queryValues(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestReadQueryControlClassification(t *testing.T) {
	if !slices.IsSorted(singleValueQueryKeys[:]) {
		t.Fatal("single-value controls must be sorted for deterministic refusals")
	}
	classified := append([]string(nil), singleValueQueryKeys[:]...)
	classified = append(classified, "sort")
	slices.Sort(classified)
	if reserved := filter.ListControlParams(); !slices.Equal(classified, reserved) {
		t.Fatalf("CRUD repeat policy %v does not cover reserved controls %v", classified, reserved)
	}
}

func TestReadQueryRepeatedOrder(t *testing.T) {
	ch, _ := covItems(t, nil, 0)
	for _, raw := range []string{
		"where=&where=&include=&include=&fields=name&fields=name",
		"fields=name&fields=name&include=&include=&where=&where=",
	} {
		_, err := ch.readQuery(httptest.NewRequest("GET", "/items?"+raw, nil))
		if err == nil || !strings.Contains(err.Error(), `"fields"`) {
			t.Errorf("%s: %v, want the alphabetically first repeated key fields", raw, err)
		}
	}
}

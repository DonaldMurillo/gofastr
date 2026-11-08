package entityui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestSingleValueEnumFormsRender(t *testing.T) {
	cases := []struct {
		name         string
		required     bool
		values       []string
		defaultValue any
		readOnly     bool
		control      string
	}{
		{"required_default", true, []string{"only"}, "only", false, "select"},
		{"required_no_default", true, []string{"only"}, nil, false, "select"},
		{"optional", false, []string{"only"}, nil, false, "select"},
		{"two_required", true, []string{"only", "other"}, "only", false, "segmented"},
		{"locked", true, []string{"only"}, "only", true, "none"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := entity.EntityConfig{
				Table: testCase.name,
				Fields: []schema.Field{
					{Name: "title", Type: schema.String, Required: true},
					{Name: "stage", Type: schema.Enum, Required: testCase.required, Values: testCase.values,
						Default: testCase.defaultValue, ReadOnly: testCase.readOnly},
				},
			}.WithTimestamps(false)
			fixture := newTestUI(t, map[string]entity.EntityConfig{testCase.name: config},
				map[string][]map[string]any{testCase.name: {{"id": "row1", "title": "Example", "stage": "only"}}},
				withAPI(map[string]string{testCase.name: "/api/" + testCase.name}))
			for screen, recordID := range map[string]string{"record": "row1", "create": ""} {
				body := string(fixture.ui.Record(testCase.name, recordID).Base("/records").
					RenderCtx(fixture.userCtx("/records/"+screen, "", "user1")))
				if strings.Contains(body, "fui-callout--danger") {
					t.Fatalf("%s: form replaced by a failure callout: %s", screen, body)
				}
				if !strings.Contains(body, `name="title"`) {
					t.Errorf("%s: title input missing", screen)
				}
				selectHTML := regexp.MustCompile(`(?s)<select[^>]*name="stage"[^>]*>.*?</select>`).FindString(body)
				segmented := strings.Contains(body, "fui-segmented")
				switch testCase.control {
				case "select":
					if selectHTML == "" || segmented {
						t.Fatalf("%s: want a stage select and no segmented control: %s", screen, body)
					}
					options := regexp.MustCompile(`<option[^>]*value="([^"]*)"`).FindAllStringSubmatch(selectHTML, -1)
					var values []string
					for _, option := range options {
						values = append(values, option[1])
					}
					want := "only"
					if !testCase.required {
						want = ",only"
					}
					if got := strings.Join(values, ","); got != want {
						t.Errorf("%s: stage options = %q, want %q", screen, got, want)
					}
					opening := regexp.MustCompile(`<select[^>]*>`).FindString(selectHTML)
					if strings.Contains(opening, "required") != testCase.required {
						t.Errorf("%s: select required = %v, want %v: %s", screen, !testCase.required, testCase.required, opening)
					}
				case "segmented":
					if !segmented || selectHTML != "" || !strings.Contains(body, `name="stage"`) {
						t.Errorf("%s: want the segmented stage control: %s", screen, body)
					}
				case "none":
					if strings.Contains(body, `name="stage"`) {
						t.Errorf("%s: a locked stage drew an input: %s", screen, body)
					}
				}
			}
		})
	}
}

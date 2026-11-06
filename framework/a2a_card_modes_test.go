package framework

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func TestA2ACardKeepsSkillModes(t *testing.T) {
	app := NewApp(WithDB(sqliteDB(t)), WithA2A(A2AConfig{}))
	app.Entity("notes", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{MCP: true}}.WithTimestamps(false))
	host := uihost.New(uiapp.NewApp("cards"), uihost.WithAgentCard(uihost.AgentCardConfig{Name: "Cards", A2AEndpoint: "/a2a", Skills: app.A2ASkills()}))
	response := httptest.NewRecorder()
	host.ServeHTTP(response, httptest.NewRequest("GET", "/.well-known/agent-card.json", nil))
	var card struct {
		Skills []struct {
			ID          string
			InputModes  []string `json:"inputModes"`
			OutputModes []string `json:"outputModes"`
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	for _, skill := range card.Skills {
		if skill.ID == "entity.notes" {
			if !slices.Equal(skill.InputModes, []string{"application/json"}) || !slices.Equal(skill.OutputModes, []string{"application/json"}) {
				t.Errorf("entity card modes: input=%v output=%v", skill.InputModes, skill.OutputModes)
			}
			return
		}
	}
	t.Fatalf("entity skill missing: %s", response.Body.String())
}

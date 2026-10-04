package main

// screens.go renders the page from framework/ui components only: no
// CSS and no hand-rolled layout. The team list is a localentity list
// inside a ui.Grid, so each member card is one grid cell; the card is
// a server-rendered template the behaviour clones per member.

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/localentity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// formID is the form carrier's id; each card's Edit button names it.
const formID = "team-form"

// TeamScreen is the whole app: the add form and the team.
type TeamScreen struct{ component.ContextOnly }

func (s *TeamScreen) ScreenTitle() string { return "Your team" }

func (s *TeamScreen) RenderCtx(ctx context.Context) render.HTML {
	team := Members.List(localentity.ListConfig{OrderBy: "level", Desc: true})
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow:  "GoFastr example",
			Title:    "Your team",
			Subtitle: "Saved in this browser only. Nothing is sent to a server, and every open tab stays in step.",
			Badge:    ui.Muted(Members.Count(), render.Text(" of "+strconv.Itoa(TeamSize))),
		}),
		ui.Card(ui.CardConfig{Heading: "Add a Pokémon", HeadingLevel: 2,
			Description: "Pick Edit on a card to change it; Clear ends the edit."},
			Members.Form(formID, ui.Form(ui.FormConfig{
				Action:     "/",
				Method:     "POST",
				HideSubmit: true,
				Ctx:        ctx,
			},
				ui.TextField(ui.TextFieldConfig{
					Name: "nickname", Label: "Nickname", ID: "member-nickname",
					Required: true, MaxLength: 20, Placeholder: "Sparky",
				}),
				ui.Select(ui.SelectConfig{
					Name: "species", Label: "Species", ID: "member-species",
					Required: true, Placeholder: "Choose a species", Options: speciesOptions(),
				}),
				ui.NumberField(ui.NumberFieldConfig{
					Name: "level", Label: "Level", ID: "member-level",
					Required: true, Min: ptr(1), Max: ptr(100), Value: "5",
				}),
				ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Justify: ui.JustifyEnd},
					ui.Button(ui.ButtonConfig{Label: "Clear", Type: "reset", Variant: ui.ButtonGhost}),
					ui.Button(ui.ButtonConfig{Label: "Save to team", Type: "submit", Variant: ui.ButtonPrimary}),
				),
			)),
		),
		ui.Grid(ui.GridConfig{Min: "14rem"}, team.Render(
			team.Row(memberCard),
			team.Empty(ui.EmptyState(ui.EmptyStateConfig{
				Title:        "No Pokémon yet",
				Description:  "Add up to six with the form above. They stay in this browser.",
				HeadingLevel: 2,
			})),
		)),
	)
}

// memberCard is the row template: one card per team member.
func memberCard(r localentity.Row) render.HTML {
	return ui.Card(ui.CardConfig{
		HeadingContent: r.Text("nickname"),
		HeadingLevel:   2,
		Footer: ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
			r.Edit(formID, ui.Button(ui.ButtonConfig{Label: "Edit", Type: "button", Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall})),
			r.Delete(ui.Button(ui.ButtonConfig{Label: "Release", Type: "button", Variant: ui.ButtonDanger, Size: ui.ButtonSizeSmall})),
		),
	},
		ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{
			{Label: "Species", Value: r.Text("species")},
			{Label: "Level", Value: r.Text("level")},
		}}),
	)
}

func speciesOptions() []ui.SelectOption {
	out := make([]ui.SelectOption, len(Species))
	for i, s := range Species {
		out[i] = ui.SelectOption{Value: s, Text: s}
	}
	return out
}

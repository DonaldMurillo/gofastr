package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/localdb"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/localentity"
)

// TeamSize is the most Pokémon a team holds.
const TeamSize = 6

// Species is the pick list. An Enum field: the form offers exactly
// these and the behaviour refuses anything else.
var Species = []string{
	"Bulbasaur", "Charmander", "Squirtle", "Pikachu", "Eevee",
	"Jigglypuff", "Snorlax", "Gengar", "Lapras", "Dragonite",
}

// Box is the visitor's browser-side database; Members is the team.
var (
	Box = localdb.New("team-builder")

	Members = localentity.Define(Box, "members", []schema.Field{
		{Name: "nickname", Type: schema.String, Required: true, Min: ptr(1), Max: ptr(20)},
		{Name: "species", Type: schema.Enum, Required: true, Values: Species},
		{Name: "level", Type: schema.Int, Required: true, Min: ptr(1), Max: ptr(100)},
	},
		localentity.Indexed("level"),
		localentity.MaxRecords(TeamSize),
		localentity.WithMessages(localentity.Messages{
			Full: "Your team is full: release a Pokémon first ({n} at most).",
		}),
	)
)

func ptr(f float64) *float64 { return &f }

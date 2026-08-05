package main

// seedEntity is one entity's ordered seed rows.
type seedEntity struct {
	Entity string
	Rows   []map[string]any
}

// seedData returns the initial seed data in blueprint-declared
// order (so entities that reference others are inserted after them).
func seedData() []seedEntity {
	return []seedEntity{
		{Entity: "tags", Rows: []map[string]any{
			{"name": "urgent"},
			{"name": "backend"},
			{"name": "design"},
		}},
	}
}

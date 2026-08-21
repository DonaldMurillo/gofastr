package apiversions

import (
	"slices"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Projection defines which fields are visible in a specific API version.
type Projection struct {
	Version string

	// Include lists field names to include. If empty, all fields are included.
	Include []string

	// Exclude lists field names to exclude. Applied after Include.
	Exclude []string

	// Rename maps field names to different JSON keys for this version.
	Rename map[string]string
}

// ProjectionSet groups multiple projections for an entity.
type ProjectionSet struct {
	Default  *Projection
	Versions map[string]*Projection
}

// NewProjectionSet creates a projection set with the given version projections.
func NewProjectionSet(versions ...*Projection) *ProjectionSet {
	ps := &ProjectionSet{
		Versions: make(map[string]*Projection),
	}
	for _, p := range versions {
		ps.Versions[p.Version] = p
	}
	return ps
}

// For returns the projection for the given version, or the default.
func (ps *ProjectionSet) For(version string) *Projection {
	if ps == nil {
		return nil
	}
	if p, ok := ps.Versions[version]; ok {
		return p
	}
	return ps.Default
}

// ApplyToEntityConfig returns a modified EntityConfig with fields' visibility
// and wire names adjusted according to the projection for the given version.
// Does not mutate the original, and does NOT change the field set
// or the DB column names: Exclude hides a field (Hidden=true), Include hides
// everything not allow-listed, and Rename sets the wire-name override
// (WireName), the underlying table schema is identical across versions so
// two versions of one entity can share one table safely.
func ApplyToEntityConfig(base entity.EntityConfig, ps *ProjectionSet, version string) entity.EntityConfig {
	p := ps.For(version)
	if p == nil {
		return base
	}

	adjusted := make([]schema.Field, len(base.Fields))
	for i, f := range base.Fields {
		f.Hidden = f.Hidden || !shouldInclude(f.Name, p)
		if rename, ok := p.Rename[f.Name]; ok {
			f.WireName = rename
		}
		adjusted[i] = f
	}

	cfg := base
	cfg.Fields = adjusted
	return cfg
}

func shouldInclude(fieldName string, p *Projection) bool {
	if slices.Contains(p.Exclude, fieldName) {
		return false
	}
	if len(p.Include) > 0 {
		return slices.Contains(p.Include, fieldName)
	}
	return true
}

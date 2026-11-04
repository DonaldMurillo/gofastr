package migrate

import (
	"fmt"
	"maps"
	"slices"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

func checkGeneratedPivotDeclarations(ordered []*entity.Entity, all map[string]*entity.Entity, dialect Dialect) error {
	canonical := make(map[string]pivotSpec)
	for _, ent := range ordered {
		for _, rel := range ent.Config.Relations {
			if rel.Type != entity.RelManyToMany || rel.Through == "" || pivotHasEntity(rel.Through, all) {
				continue
			}
			spec, err := buildPivotSpec(ent, rel, all, dialect)
			if err != nil {
				return err
			}
			key := asciicase.Lower(spec.Safe)
			previous, exists := canonical[key]
			if !exists {
				canonical[key] = spec
				continue
			}
			if len(spec.Cols) != len(previous.Cols) {
				return fmt.Errorf("entity %q relation %q: inconsistent link columns for shared pivot table %q", ent.GetName(), rel.Name, spec.Safe)
			}
			if err := checkPivotColumns(spec, previous.Cols, dialect); err != nil {
				return fmt.Errorf("entity %q relation %q: inconsistent shared declaration: %w", ent.GetName(), rel.Name, err)
			}
		}
	}
	return nil
}

// PivotTable is one many_to_many pivot table AutoMigrate creates itself
// (no registered entity owns it): two link columns, each a foreign key
// to one end's one-column primary key, both ON DELETE CASCADE.
type PivotTable struct {
	Table       string // the relation's Through table
	LocalKey    string // pivot column referencing SourceTable(SourceKey)
	SourceTable string
	SourceKey   string
	TargetFK    string // pivot column referencing TargetTable(TargetKey)
	TargetTable string
	TargetKey   string
}

// GeneratedPivotTables lists the pivot tables AutoMigrate creates for reg,
// resolved the way the migrator and the snapshot resolve them: version
// union, dependency order, the first declaration of each Through table,
// and no table a registered entity owns. A relation the migrator refuses
// (an unknown end, a composite-keyed end) has no table and is left out.
// Data export and erasure read it so the rows in these tables, which no
// entity describes, are dumped, restored, and erased with their ends.
func GeneratedPivotTables(reg entity.Registry) []PivotTable {
	if reg == nil {
		return nil
	}
	all := UnionEntities(reg)
	ordered, err := topoSortEntities(all)
	if err != nil {
		ordered = ordered[:0]
		for _, n := range slices.Sorted(maps.Keys(all)) {
			ordered = append(ordered, all[n])
		}
	}
	var out []PivotTable
	for ent, rel := range generatedPivots(ordered, all) {
		target := all[rel.Entity]
		if target == nil {
			continue
		}
		sourceKey, err := ent.KeyColumn()
		if err != nil {
			continue
		}
		targetKey, err := target.KeyColumn()
		if err != nil {
			continue
		}
		out = append(out, PivotTable{
			Table:       rel.Through,
			LocalKey:    rel.LocalKey,
			SourceTable: ent.GetTable(),
			SourceKey:   sourceKey,
			TargetFK:    rel.ForeignKeyTarget,
			TargetTable: target.GetTable(),
			TargetKey:   targetKey,
		})
	}
	return out
}

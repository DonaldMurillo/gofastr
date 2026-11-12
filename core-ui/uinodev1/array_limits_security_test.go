package uinodev1

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestPropArraysAreBounded(t *testing.T) {
	for _, component := range []string{"rows", "columns", "cells", "detail-list", "key-value"} {
		for _, length := range []int{128, 129} {
			t.Run(component+"/"+strconv.Itoa(length), func(t *testing.T) {
				columns := []DataColumn{{Key: "id", Label: "ID"}}
				rows := []DataRow{{Cells: []DataCell{{Text: ""}}}}
				props := any(DataTableProps{Columns: columns, Rows: rows})
				kind := "data-table"
				switch component {
				case "rows":
					props = DataTableProps{Columns: columns, Rows: make([]DataRow, length)}
					for index := range props.(DataTableProps).Rows {
						props.(DataTableProps).Rows[index] = rows[0]
					}
				case "columns":
					many := make([]DataColumn, length)
					for index := range many {
						key := "c" + strconv.Itoa(index)
						many[index] = DataColumn{Key: key, Label: key}
					}
					props = DataTableProps{Columns: many, Rows: rows}
				case "cells":
					props = DataTableProps{Columns: columns, Rows: []DataRow{{Cells: make([]DataCell, length)}}}
				case "detail-list":
					items := make([]DetailItem, length)
					for index := range items {
						items[index] = DetailItem{Label: "a", Value: "b"}
					}
					kind, props = component, DetailListProps{Items: items}
				case "key-value":
					items := make([]KeyValueItem, length)
					for index := range items {
						items[index] = KeyValueItem{Key: "a", Value: "b"}
					}
					kind, props = component, KeyValueProps{Items: items}
				}
				data, err := json.Marshal(map[string]any{"component": kind, "props": props})
				if err != nil {
					t.Fatal(err)
				}
				tree, err := Validate(data, Limits{})
				if length == 128 && err != nil {
					t.Fatalf("boundary array rejected: %v", err)
				}
				if length == 129 && (err == nil || tree != nil) {
					t.Fatalf("oversized %s array accepted (length %d)", component, length)
				}
			})
		}
	}
}

func TestPropArrayLimitUsesOverride(t *testing.T) {
	data := []byte(`{"component":"data-table","props":{"columns":[{"key":"id","label":"ID"}],"rows":[{"cells":[{"text":""}]},{"cells":[{"text":""}]}]}}`)
	for _, limit := range []int{1, 2} {
		_, err := Validate(data, Limits{MaxPropItems: limit})
		if (err == nil) != (limit == 2) {
			t.Errorf("two rows with MaxPropItems=%d: %v", limit, err)
		}
	}
}

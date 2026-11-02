package crud

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A field whose name does not start lower-case (Title, HTTPCode, _note)
// is served under the wire key Title, HTTPCode, Note. A struct tagged with
// those keys went through MarshalEntity's snake_case (title,
// h_t_t_p_code, note), which named no column, so CreateOne and UpdateOne
// dropped the value and returned the row with the field empty. Each such
// key now maps back to its column, on both dialects, and the row reads
// back into the same struct.
func TestMarshalEntityKeepsNonLowerFields(t *testing.T) {
	type post struct {
		ID       string  `json:"id,omitempty"`
		Title    *string `json:"Title,omitempty"`
		HTTPCode *int64  `json:"HTTPCode,omitempty"`
		Note     *string `json:"Note,omitempty"`
	}
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE cposts (id TEXT PRIMARY KEY, Title TEXT, HTTPCode BIGINT, _note TEXT)`); err != nil {
			t.Fatal(err)
		}
		ent := entity.Define("cposts", entity.EntityConfig{
			Name:  "cposts",
			Table: "cposts",
			Fields: []schema.Field{
				{Name: "Title", Type: schema.String},
				{Name: "HTTPCode", Type: schema.Int},
				{Name: "_note", Type: schema.String},
			},
		}.WithTimestamps(false))
		ent.SetDB(db)
		ch := NewCrudHandler(ent, db)
		ctx := t.Context()

		title, code, note := "kept", int64(418), "aside"
		body, err := MarshalEntity(post{Title: &title, HTTPCode: &code, Note: &note})
		if err != nil {
			t.Fatal(err)
		}
		row, err := ch.CreateOne(ctx, body)
		if err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		id := fmt.Sprint(row["id"])
		got, err := ch.GetOne(ctx, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		var back post
		if err := UnmarshalEntity(got, &back); err != nil {
			t.Fatal(err)
		}
		if back.Title == nil || *back.Title != title || back.HTTPCode == nil || *back.HTTPCode != code || back.Note == nil || *back.Note != note {
			t.Fatalf("create read back %#v from row %v", back, got)
		}

		title2 := "changed"
		patch, err := MarshalEntity(post{Title: &title2})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ch.UpdateOne(ctx, id, patch); err != nil {
			t.Fatalf("UpdateOne: %v", err)
		}
		got, err = ch.GetOne(ctx, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got["Title"] != title2 {
			t.Fatalf("update read back %v", got)
		}
	})
}

// A cascade relation named Lines or _extras is written under its wire key
// Lines or Extras, which MarshalEntity snakes to lines and extras. The
// second named no relation, so its children were dropped.
func TestMarshalEntityKeepsNonLowerRelation(t *testing.T) {
	type line struct {
		Body string `json:"body"`
	}
	type order struct {
		Ref    string `json:"ref"`
		Lines  []line `json:"Lines,omitempty"`
		Extras []line `json:"Extras,omitempty"`
	}
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		for _, ddl := range []string{
			`CREATE TABLE corders (id TEXT PRIMARY KEY, ref TEXT)`,
			`CREATE TABLE clines (id TEXT PRIMARY KEY, order_id TEXT, body TEXT)`,
			`CREATE TABLE cextras (id TEXT PRIMARY KEY, order_id TEXT, body TEXT)`,
		} {
			if _, err := db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
		}
		public := &entity.ExposureConfig{Public: true}
		lines := entity.Define("clines", entity.EntityConfig{
			Table:    "clines",
			Fields:   []schema.Field{{Name: "order_id", Type: schema.String}, {Name: "body", Type: schema.String}},
			Exposure: public,
		}.WithTimestamps(false))
		extras := entity.Define("cextras", entity.EntityConfig{
			Table:    "cextras",
			Fields:   []schema.Field{{Name: "order_id", Type: schema.String}, {Name: "body", Type: schema.String}},
			Exposure: public,
		}.WithTimestamps(false))
		orders := entity.Define("corders", entity.EntityConfig{
			Table:  "corders",
			Fields: []schema.Field{{Name: "ref", Type: schema.String}},
			Relations: []entity.Relation{
				entity.HasMany("Lines", "clines", "order_id").WithCascadeWrite(true),
				entity.HasMany("_extras", "cextras", "order_id").WithCascadeWrite(true),
			},
			Exposure: public,
		}.WithTimestamps(false))
		lines.SetDB(db)
		extras.SetDB(db)
		orders.SetDB(db)
		ch := NewCrudHandler(orders, db)
		ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"clines": lines, "cextras": extras, "corders": orders}}
		body, err := MarshalEntity(order{Ref: "o1", Lines: []line{{Body: "first"}}, Extras: []line{{Body: "extra"}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ch.CreateOne(t.Context(), body); err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		for table, body := range map[string]string{"clines": "first", "cextras": "extra"} {
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE body = $1`, body).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Errorf("%s cascade child rows = %d, want 1", table, n)
			}
		}
	})
}

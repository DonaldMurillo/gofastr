package entityui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A datetime-local input has no zone, and the write reads its value as
// UTC. The form drew a stored instant in whatever zone it arrived in (a
// time.Time in its own location, a stored string's first 16 characters),
// so a value read back at +02:00 showed 12:00 for 10:00Z and an
// untouched save moved it two hours. The input shows UTC wall time,
// with seconds when the instant has them.
func TestTimestampInputIsUTC(t *testing.T) {
	f := schema.Field{Name: "at", Type: schema.Timestamp}
	plus2 := time.FixedZone("plus2", 2*3600)
	for _, tc := range []struct {
		in   any
		want string
	}{
		{time.Date(2026, 10, 27, 12, 0, 0, 0, plus2), "2026-10-27T10:00"},
		{time.Date(2026, 10, 27, 12, 0, 30, 0, plus2), "2026-10-27T10:00:30"},
		{"2026-10-27T12:00:00+02:00", "2026-10-27T10:00"},
		{"2026-10-27T10:00:00.000000Z", "2026-10-27T10:00"},
		{"2026-10-27T10:00:30.000000Z", "2026-10-27T10:00:30"},
		{"2026-10-27T10:00", "2026-10-27T10:00"},
		{"not a time", ""},
	} {
		if got := timestampInputValue(formValueText(f, tc.in)); got != tc.want {
			t.Errorf("input value for %v = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// On Postgres the driver scans TIMESTAMPTZ in the session's zone. A
// value written from the form, read back, drawn and saved again names
// the instant it started with.
func TestPGTimestampFormRoundTrip(t *testing.T) {
	db := pgtest.DB(t)
	if _, err := db.Exec(`SET TIME ZONE 'Asia/Kolkata'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE things (id SERIAL PRIMARY KEY, occurred_at TIMESTAMPTZ)`); err != nil {
		t.Fatal(err)
	}
	f := schema.Field{Name: "occurred_at", Type: schema.Timestamp}
	ent := entity.Define("things", entity.EntityConfig{
		Table:    "things",
		Fields:   []schema.Field{{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement}, f},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := crud.NewCrudHandler(ent, db)
	ctx := asUser(context.Background(), "u1")
	row, err := ch.CreateOne(ctx, map[string]any{"occurred_at": "2026-10-27T10:00"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := fmt.Sprint(row["id"])
	want := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	for range 2 {
		got, err := ch.GetOne(ctx, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		v := (&meta{}).rowValue(got, "occurred_at")
		shown := timestampInputValue(formValueText(f, v))
		if shown != "2026-10-27T10:00" {
			t.Fatalf("form shows %q for %#v, want the UTC wall time", shown, v)
		}
		if _, err := ch.UpdateOne(ctx, id, map[string]any{"occurred_at": shown}); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	var stored time.Time
	if err := db.QueryRow(`SELECT occurred_at FROM things WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !stored.Equal(want) {
		t.Fatalf("after two untouched saves the column holds %s, want %s", stored, want)
	}
}

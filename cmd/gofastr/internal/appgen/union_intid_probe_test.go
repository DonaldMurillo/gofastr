package appgen

// intIDProbe joins the union app's main package; TestUnionApp writes it
// in. Its tickets key on an integer id the database assigns, and their
// replies, keyed the same way, cascade from them. Consumer code passes the
// id a repo row carries straight back to every repo method, the id
// column, the batch calls, a cascade child and the HTTP client, and reads
// the deleted event's id as the same type: it compiles only while the
// generated API types the id one way. A nested update then addresses a
// cascade tag by its own key column.
const intIDProbe = `package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"example.com/union/entities"
	"example.com/union/entities/client"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestIntIDRepo(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	entities.RegisterAll(app)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	ctx := context.Background()
	tickets := entities.NewTicketsRepo(app)
	deleted := make(chan int, 8)
	defer entities.OnTicketsDeleted(app, func(_ context.Context, id int) error {
		deleted <- id
		return nil
	})()

	row, err := tickets.Create(ctx, entities.TicketsInput{Subject: "a"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var id int = row.ID
	if id == 0 {
		t.Fatalf("create returned no id: %+v", row)
	}
	if got, err := tickets.Get(ctx, row.ID); err != nil || got.Subject != "a" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if got, err := tickets.Query().Where(entities.TicketsID.Eq(row.ID)).First(ctx); err != nil || got.ID != row.ID {
		t.Fatalf("query by id = %+v, %v", got, err)
	}
	if ok, err := tickets.Exists(ctx, row.ID); err != nil || !ok {
		t.Fatalf("exists = %v, %v", ok, err)
	}
	if got, err := tickets.Update(ctx, row.ID, entities.TicketsPatch{Subject: new("b")}); err != nil || got.Subject != "b" {
		t.Fatalf("update = %+v, %v", got, err)
	}
	made, err := tickets.BatchCreate(ctx, []entities.TicketsInput{{Subject: "c"}, {Subject: "d"}})
	if err != nil || len(made) != 2 {
		t.Fatalf("batch create = %+v, %v", made, err)
	}
	if got, err := tickets.BatchUpdate(ctx, []int{made[0].ID}, []entities.TicketsPatch{{Subject: new("c2")}}); err != nil || got[0].Subject != "c2" {
		t.Fatalf("batch update = %+v, %v", got, err)
	}
	if err := tickets.BatchDelete(ctx, []int{made[0].ID, made[1].ID}); err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	if err := tickets.Delete(ctx, row.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ok, err := tickets.Exists(ctx, row.ID); err != nil || ok {
		t.Fatalf("exists after delete = %v, %v", ok, err)
	}
	select {
	case got := <-deleted:
		if got != made[0].ID && got != made[1].ID && got != row.ID {
			t.Errorf("deleted event id %d names no deleted ticket", got)
		}
	case <-time.After(5 * time.Second):
		t.Error("OnTicketsDeleted never fired")
	}

	// A cascade child's ID is the child's id type.
	parent, err := tickets.Create(ctx, entities.TicketsInput{Subject: "p",
		Replies: []entities.TicketRepliesInput{{Body: "r1"}}})
	if err != nil {
		t.Fatalf("cascade create: %v", err)
	}
	replies := entities.NewTicketRepliesRepo(app)
	kids, err := replies.Query().Where(entities.TicketRepliesTicketId.Eq(parent.ID)).Find(ctx)
	if err != nil || len(kids) != 1 {
		t.Fatalf("cascaded replies = %+v, %v", kids, err)
	}
	if _, err := tickets.Update(ctx, parent.ID, entities.TicketsPatch{Replies: []entities.TicketsRepliesNested{
		{ID: kids[0].ID, TicketRepliesPatch: entities.TicketRepliesPatch{Body: new("r1 edited")}},
	}}); err != nil {
		t.Fatalf("cascade update: %v", err)
	}
	if got, err := replies.Get(ctx, kids[0].ID); err != nil || got.Body != "r1 edited" {
		t.Fatalf("reply after cascade update = %+v, %v", got, err)
	}

	// A cascade child keyed on its own column is addressed by it.
	tagged, err := tickets.Create(ctx, entities.TicketsInput{Subject: "t",
		Tags: []entities.TicketTagsInput{{TagCode: "c1", Label: new("one")}}})
	if err != nil {
		t.Fatalf("cascade create tag: %v", err)
	}
	if _, err := tickets.Update(ctx, tagged.ID, entities.TicketsPatch{Tags: []entities.TicketsTagsNested{
		{TagCode: "c1", TicketTagsPatch: entities.TicketTagsPatch{Label: new("one edited")}},
	}}); err != nil {
		t.Fatalf("cascade update tag: %v", err)
	}
	tags := entities.NewTicketTagsRepo(app)
	if got, err := tags.Get(ctx, "c1"); err != nil || got.Label != "one edited" {
		t.Fatalf("tag after cascade update = %+v, %v", got, err)
	}

	// The HTTP client takes the id its rows carry.
	_ = func(c *client.Client) error {
		got, err := c.GetTickets(ctx, row.ID)
		if err != nil {
			return err
		}
		if err := c.DeleteTickets(ctx, got.ID); err != nil {
			return err
		}
		_, err = c.BatchDeleteTickets(ctx, []int{got.ID})
		return err
	}
}
`

package main

// The data layer: three entities declared through the framework, stored
// in SQLite, read through typed queries. The app serves no CRUD routes;
// the pages are the only way in, and they only read.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// User is one account.
type User struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Karma   int    `json:"karma"`
	About   string `json:"about"`
}

// Story is one submission: a link, an Ask, a Show or a job.
type Story struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Domain      string  `json:"domain"`
	Author      string  `json:"author"`
	Posted      int64   `json:"posted"`
	Points      int     `json:"points"`
	Descendants int     `json:"descendants"`
	Text        string  `json:"body"`
	Hot         float64 `json:"hot"`
}

// CommentRow is one stored comment; ParentID is empty for a top-level
// comment.
type CommentRow struct {
	ID       string `json:"id"`
	StoryID  string `json:"storyId"`
	ParentID string `json:"parentId"`
	Author   string `json:"author"`
	Posted   int64  `json:"posted"`
	Body     string `json:"body"`
}

// registerEntities declares the three tables. CRUD routes are off: the
// dataset is read-only and the HTML pages are the whole interface.
func registerEntities(app *framework.App) {
	app.Entity("users", framework.EntityConfig{
		Exposure:   &framework.ExposureConfig{CRUD: new(false)},
		Timestamps: new(false),
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "created", Type: schema.Int},
			{Name: "karma", Type: schema.Int},
			{Name: "about", Type: schema.Text},
		},
	})
	app.Entity("stories", framework.EntityConfig{
		Exposure:   &framework.ExposureConfig{CRUD: new(false)},
		Timestamps: new(false),
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "kind", Type: schema.Enum, Values: []string{"story", "ask", "show", "job"}, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "url", Type: schema.String},
			{Name: "domain", Type: schema.String},
			{Name: "author", Type: schema.String},
			{Name: "posted", Type: schema.Int},
			{Name: "points", Type: schema.Int},
			{Name: "descendants", Type: schema.Int},
			{Name: "body", Type: schema.Text},
			{Name: "hot", Type: schema.Float},
		},
		Indices: []framework.Index{
			{Columns: []string{"kind", "hot"}},
			{Columns: []string{"kind", "posted"}},
			{Columns: []string{"hot"}},
			{Columns: []string{"posted"}},
		},
	})
	app.Entity("comments", framework.EntityConfig{
		Exposure:   &framework.ExposureConfig{CRUD: new(false)},
		Timestamps: new(false),
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "story_id", Type: schema.String, Required: true},
			{Name: "parent_id", Type: schema.String},
			{Name: "author", Type: schema.String},
			{Name: "posted", Type: schema.Int},
			{Name: "body", Type: schema.Text},
		},
		Indices: []framework.Index{{Columns: []string{"story_id", "posted"}}},
	})
}

// store reads the dataset. One handler per entity; the typed queries
// built on them decode rows into the structs above.
type store struct {
	users, stories, comments *crud.CrudHandler
}

func newStore(app *framework.App) (*store, error) {
	h := func(name string) (*crud.CrudHandler, error) {
		e, err := app.Registry.Get(name)
		if err != nil {
			return nil, fmt.Errorf("hn: entity %s: %w", name, err)
		}
		return framework.NewCrudHandler(e, app.DB), nil
	}
	var s store
	var err error
	if s.users, err = h("users"); err != nil {
		return nil, err
	}
	if s.stories, err = h("stories"); err != nil {
		return nil, err
	}
	if s.comments, err = h("comments"); err != nil {
		return nil, err
	}
	return &s, nil
}

// errNotFound is what a lookup of a missing row returns.
var errNotFound = errors.New("hn: not found")

var (
	colKind   = framework.NewStringColumn("kind")
	colHot    = framework.NewFloatColumn("hot")
	colPosted = framework.NewIntColumn("posted")
	colID     = framework.NewStringColumn("id")
	colStory  = framework.NewStringColumn("story_id")
	colAuthor = framework.NewStringColumn("author")
)

// list names one of the story lists and how it is ranked.
type list struct {
	// Path is the list's own route.
	Path string
	// Title is the page title and the list's label.
	Title string
	// Kind narrows to one kind of story; empty is every kind but jobs.
	Kind string
	// ByTime ranks newest first; otherwise by hotness.
	ByTime bool
	// Cap stops the list after this many stories (the top list holds
	// 300, like the real one).
	Cap int
}

var (
	listTop    = list{Path: "/", Title: "Top stories", Cap: topPages * perPage}
	listNewest = list{Path: "/newest", Title: "New stories", ByTime: true}
	listAsk    = list{Path: "/ask", Title: "Ask", Kind: "ask"}
	listShow   = list{Path: "/show", Title: "Show", Kind: "show"}
	listJobs   = list{Path: "/jobs", Title: "Jobs", Kind: "job", ByTime: true}
)

// page reads one page of a list and the list's page count. A page past
// the end is errNotFound.
func (s *store) page(ctx context.Context, l list, page int) ([]*Story, int, error) {
	q := func() *crud.TypedQuery[Story] {
		q := framework.NewTypedQuery[Story](s.stories)
		if l.Kind != "" {
			q.Where(colKind.Eq(l.Kind))
		} else {
			q.Where(colKind.Neq("job"))
		}
		return q
	}
	total, err := q().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	if l.Cap > 0 {
		total = min(total, l.Cap)
	}
	pages := max(1, (total+perPage-1)/perPage)
	if page < 1 || page > pages {
		return nil, pages, errNotFound
	}
	rows := q()
	if l.ByTime {
		rows.Order(colPosted.Desc())
	} else {
		rows.Order(colHot.Desc())
	}
	limit := min(perPage, total-(page-1)*perPage)
	out, err := rows.Order(colID.Desc()).Limit(limit).Offset((page - 1) * perPage).Find(ctx)
	return out, pages, err
}

// story reads one story.
func (s *store) story(ctx context.Context, id string) (*Story, error) {
	return first(framework.NewTypedQuery[Story](s.stories).Where(colID.Eq(id)).First(ctx))
}

// user reads one user.
func (s *store) user(ctx context.Context, id string) (*User, error) {
	return first(framework.NewTypedQuery[User](s.users).Where(colID.Eq(id)).First(ctx))
}

// thread reads a story's comments, oldest first.
func (s *store) thread(ctx context.Context, storyID string) ([]*CommentRow, error) {
	return framework.NewTypedQuery[CommentRow](s.comments).
		Where(colStory.Eq(storyID)).
		Order(colPosted.Asc()).Order(colID.Asc()).
		Find(ctx)
}

// submissions counts what a user posted.
func (s *store) submissions(ctx context.Context, author string) (int, error) {
	return framework.NewTypedQuery[Story](s.stories).Where(colAuthor.Eq(author)).Count(ctx)
}

func first[T any](v *T, err error) (*T, error) {
	if err != nil {
		if crud.IsNotFound(err) || errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, err
	}
	return v, nil
}

// seed writes the generated dataset when the tables are empty. It goes
// through the entities' own create path, one transaction per table.
func (s *store) seed(ctx context.Context) error {
	n, err := framework.NewTypedQuery[User](s.users).Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	d := generate()
	if err := batch(ctx, s.users, d.users); err != nil {
		return fmt.Errorf("hn: seed users: %w", err)
	}
	if err := batch(ctx, s.stories, d.stories); err != nil {
		return fmt.Errorf("hn: seed stories: %w", err)
	}
	if err := batch(ctx, s.comments, d.comments); err != nil {
		return fmt.Errorf("hn: seed comments: %w", err)
	}
	return nil
}

// rower is a struct that writes itself as a create body, keyed by
// column name.
type rower interface{ row() map[string]any }

func (u User) row() map[string]any {
	return map[string]any{"id": u.ID, "created": u.Created, "karma": u.Karma, "about": u.About}
}

func (s Story) row() map[string]any {
	return map[string]any{
		"id": s.ID, "kind": s.Kind, "title": s.Title, "url": s.URL, "domain": s.Domain,
		"author": s.Author, "posted": s.Posted, "points": s.Points,
		"descendants": s.Descendants, "body": s.Text, "hot": s.Hot,
	}
}

func (c CommentRow) row() map[string]any {
	return map[string]any{
		"id": c.ID, "story_id": c.StoryID, "parent_id": c.ParentID,
		"author": c.Author, "posted": c.Posted, "body": c.Body,
	}
}

// batch inserts rows in chunks, each chunk one transaction.
func batch[T rower](ctx context.Context, h *crud.CrudHandler, rows []T) error {
	for chunk := range slices.Chunk(rows, 1000) {
		bodies := make([]map[string]any, len(chunk))
		for i, r := range chunk {
			bodies[i] = r.row()
		}
		if _, err := h.BatchCreateMany(ctx, bodies); err != nil {
			return err
		}
	}
	return nil
}

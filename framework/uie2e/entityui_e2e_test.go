package uie2e

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"testing"
	"time"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

type entityUIE2EScreen struct {
	component.ContextOnly
	UI   *entityui.UI
	Mode string
	id   string
}

func (s *entityUIE2EScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *entityUIE2EScreen) RenderCtx(ctx context.Context) render.HTML {
	switch s.Mode {
	case "list":
		return s.UI.List("deals").Base("/deals").Duplicate().RenderCtx(ctx)
	case "create":
		return s.UI.Create("deals").Base("/deals").RenderCtx(ctx)
	default:
		return s.UI.Record("deals", s.id).Base("/deals").Duplicate().RenderCtx(ctx)
	}
}

func entityUIE2EApp(t *testing.T, tune func(*entityui.UI) *entityui.UI) (*httptest.Server, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	fw := framework.NewApp(framework.WithDB(db), framework.WithAPIPrefix("/api"))
	t.Cleanup(func() { _ = fw.Shutdown(context.Background()) })
	fw.Entity("deals", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{Public: true},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "stage", Type: schema.Enum, Values: []string{"lead", "qualified"}, Default: "lead"},
		},
		Display: &entity.DisplayConfig{TitleFields: []string{"title"}},
		States: &entity.StatesConfig{Field: "stage", Initial: []string{"lead"}, Transitions: []entity.Transition{
			{Key: "qualify", Label: "Qualify", From: []string{"lead"}, To: "qualified"},
		}},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, fw.Registry); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO deals (id, title, stage) VALUES ('d1', 'First deal', 'lead')"); err != nil {
		t.Fatal(err)
	}
	ui := fw.EntityUI(entityui.Extensions{})
	if tune != nil {
		ui = tune(ui)
	}
	site := uiapp.NewApp("Entity UI regression")
	site.Register("/deals", &entityUIE2EScreen{UI: ui, Mode: "list"}, nil)
	site.Register("/deals/new", &entityUIE2EScreen{UI: ui, Mode: "create"}, nil)
	site.Register("/deals/{id}", &entityUIE2EScreen{UI: ui}, nil)
	uihost.New(site).Mount(fw.Router())
	srv := httptest.NewServer(fw.Router())
	t.Cleanup(srv.Close)
	return srv, db
}

// Click the actual entityui button through the runtime and real CRUD route.
// The route requires JSON content type even though this POST takes no body.
func TestEntityUIStateMoveBrowser(t *testing.T) {
	srv, db := entityUIE2EApp(t, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
	selector := `[data-cui-rpc="/api/deals/d1/transitions/qualify"]`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/deals/d1"),
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('main').textContent.includes('Qualified')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	var stage string
	if err := db.QueryRow("SELECT stage FROM deals WHERE id = 'd1'").Scan(&stage); err != nil {
		t.Fatal(err)
	}
	if stage != "qualified" {
		t.Fatalf("stage = %q, want qualified", stage)
	}
}

func TestEntityUICustomCreatePathBrowser(t *testing.T) {
	srv, db := entityUIE2EApp(t, func(ui *entityui.UI) *entityui.UI {
		return ui.WithCreatePath(func(e *entity.Entity) (string, bool) {
			return "/" + e.GetName() + "/new", true
		})
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/deals"),
		chromedp.WaitVisible(`a[href="/deals/new"]`, chromedp.ByQuery),
		chromedp.Click(`a[href="/deals/new"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`form[data-cui-rpc="/api/deals"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="title"]`, "Second deal", chromedp.ByQuery),
		chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
		chromedp.Poll(`location.pathname === '/deals' && document.querySelector('main').textContent.includes('Second deal')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM deals WHERE title = 'Second deal'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("created %d matching deals, want 1", count)
	}
	// Duplicate and Create another use the same route on the record screen.
	var links bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/deals/d1"),
		chromedp.Poll(`!!document.querySelector('a[href="/deals/new?duplicate=d1"]') && !!document.querySelector('a[href="/deals/new"]')`, &links, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil || !links {
		t.Fatalf("record create links missed the configured path: %v", err)
	}
}

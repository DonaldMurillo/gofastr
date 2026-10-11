package admin

import (
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var undoAttr = regexp.MustCompile(`data-cui-rpc-method="DELETE"[^>]*data-cui-rpc-success-action="([^"]*)"|data-cui-rpc-success-action="([^"]*)"[^>]*data-cui-rpc-method="DELETE"`)

// The admin's Delete on a soft-deleting entity toasts Undo from the list
// row and the record, and the restore it names brings the row back.
func TestAdminDeleteOffersUndo(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": trashedPosts()}, Config{Entities: []string{"posts"}}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "Keep me", "status": "draft"})
	h := withPolicy(x.as(theAdmin))

	for _, path := range []string{"/admin/entities/posts", "/admin/entities/posts/p1"} {
		m := undoAttr.FindStringSubmatch(get(h, path).Body.String())
		if m == nil {
			t.Fatalf("%s: the Delete carries no Undo", path)
		}
		var a struct {
			Label string            `json:"label"`
			Attrs map[string]string `json:"attrs"`
		}
		if err := json.Unmarshal([]byte(html.UnescapeString(m[1]+m[2])), &a); err != nil {
			t.Fatal(err)
		}
		if a.Label != "Undo" || a.Attrs["data-cui-rpc"] != "/admin/api/posts/p1/_restore" {
			t.Fatalf("%s: Undo = %+v, want the admin restore", path, a)
		}
	}

	if rr := serve(h, jsonReq(http.MethodDelete, "/admin/api/posts/p1", "")); rr.Code/100 != 2 {
		t.Fatalf("admin delete = %d %s", rr.Code, rr.Body.String())
	}
	if body := get(h, "/admin/entities/posts").Body.String(); strings.Contains(body, "Keep me") {
		t.Fatal("the deleted row still lists")
	}
	if rr := serve(h, jsonReq(http.MethodPost, "/admin/api/posts/p1/_restore", `{}`)); rr.Code != http.StatusOK {
		t.Fatalf("Undo's restore = %d %s", rr.Code, rr.Body.String())
	}
	if body := get(h, "/admin/entities/posts").Body.String(); !strings.Contains(body, "Keep me") {
		t.Fatal("Undo did not bring the row back")
	}
}

// The bulk bar asks for Undo, and a bulk delete's toast restores the
// rows it deleted through the admin's own bulk route.
func TestAdminBulkDeleteOffersUndo(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": trashedPosts()}, Config{Entities: []string{"posts"}}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "Keep me", "status": "draft"})
	x.insert("posts", map[string]any{"id": "p2", "title": "Me too", "status": "draft"})
	h := withPolicy(x.as(theAdmin))

	list := get(h, "/admin/entities/posts").Body.String()
	if !strings.Contains(list, `name="undo" type="hidden" value="1"`) || !strings.Contains(list, `name="back" type="hidden" value="/admin/entities/posts"`) {
		t.Fatal("the admin's bulk bar does not ask for Undo")
	}
	rr := serve(h, jsonReq(http.MethodPost, "/admin/api/posts/_bulk",
		`{"action":"delete","scope":"selected","ids":["p1","p2"],"undo":"1","back":"/admin/entities/posts"}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("bulk delete = %d %s", rr.Code, rr.Body.String())
	}
	var toasts []struct {
		Action struct {
			Label string            `json:"label"`
			Attrs map[string]string `json:"attrs"`
		} `json:"action"`
	}
	if err := json.Unmarshal([]byte(rr.Header().Get("X-Gofastr-Toast")), &toasts); err != nil || len(toasts) != 1 {
		t.Fatalf("toast header = %q (%v)", rr.Header().Get("X-Gofastr-Toast"), err)
	}
	undo := toasts[0].Action
	if undo.Label != "Undo" || undo.Attrs["data-cui-rpc"] != "/admin/api/posts/_bulk" || undo.Attrs["data-cui-rpc-navigate"] != "/admin/entities/posts" {
		t.Fatalf("Undo = %+v, want the admin bulk route back to the list", undo)
	}
	if body := get(h, "/admin/entities/posts").Body.String(); strings.Contains(body, "Keep me") || strings.Contains(body, "Me too") {
		t.Fatal("the deleted rows still list")
	}
	if rr := serve(h, jsonReq(http.MethodPost, "/admin/api/posts/_bulk", undo.Attrs["data-cui-rpc-body"])); rr.Code != http.StatusOK {
		t.Fatalf("Undo's restore = %d %s", rr.Code, rr.Body.String())
	}
	if body := get(h, "/admin/entities/posts").Body.String(); !strings.Contains(body, "Keep me") || !strings.Contains(body, "Me too") {
		t.Fatal("Undo did not bring both rows back")
	}
}

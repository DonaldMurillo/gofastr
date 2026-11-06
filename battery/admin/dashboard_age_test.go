package admin

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestCountKeepsUpdatedAge(t *testing.T) {
	world := setup(t, map[string]entity.EntityConfig{"posts": postsConfig().WithTimestamps(true)}, Config{Entities: []string{"posts"}}, nil)
	world.insert("posts", map[string]any{"id": "recent", "title": "Recent", "status": "draft", "updated_at": time.Now().UTC().Format(time.RFC3339)})
	previous := countDeadline
	countDeadline = 0
	t.Cleanup(func() { countDeadline = previous })
	for _, path := range []string{"/admin", "/admin/_count/posts"} {
		response := get(world.as(theAdmin), path)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Updated ") {
			t.Errorf("%s: status %d, recovered count lost its updated age", path, response.Code)
		}
	}
}

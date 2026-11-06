package crud

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/teststorage"
)

func TestMultipartWireNames(t *testing.T) {
	for _, collision := range []string{"", "value", "file", "mixed"} {
		t.Run(collision, func(t *testing.T) {
			database := setupDB(t, `CREATE TABLE media (id TEXT PRIMARY KEY, photo_raw TEXT, is_ready INTEGER, quantity INTEGER)`)
			model := entity.Define("media", entity.EntityConfig{Table: "media", Fields: []schema.Field{
				{Name: "photo_raw", WireName: "photo", Type: schema.Image},
				{Name: "is_ready", WireName: "ready", Type: schema.Bool},
				{Name: "quantity", WireName: "qty", Type: schema.Int},
			}}.WithTimestamps(false))
			model.SetDB(database)
			handler := NewCrudHandler(model, database)
			storageDirectory := t.TempDir()
			handler.Storage = teststorage.New(t, storageDirectory)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			if err := writer.WriteField("ready", "false"); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteField("ready", "true"); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteField("qty", "7"); err != nil {
				t.Fatal(err)
			}
			if collision == "value" {
				if err := writer.WriteField("quantity", "8"); err != nil {
					t.Fatal(err)
				}
			}
			if collision == "mixed" {
				if err := writer.WriteField("photo_raw", "old.png"); err != nil {
					t.Fatal(err)
				}
			}
			fileNames := []string{"photo"}
			if collision == "file" {
				fileNames = append(fileNames, "photo_raw")
			}
			for _, name := range fileNames {
				part, err := writer.CreateFormFile(name, "picture.png")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(pngBytes()); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			request := withTestUser(httptest.NewRequest(http.MethodPost, "/media", &body), "user")
			request.Header.Set("Content-Type", writer.FormDataContentType())
			response := httptest.NewRecorder()
			handler.Create().ServeHTTP(response, request)
			if collision != "" {
				if response.Code != http.StatusBadRequest {
					t.Fatalf("aliased %s collision = %d: %s", collision, response.Code, response.Body.String())
				}
				var count int
				if err := database.QueryRow("SELECT count(*) FROM media").Scan(&count); err != nil || count != 0 {
					t.Fatalf("refused alias collision wrote %d rows: %v", count, err)
				}
				if entries, err := os.ReadDir(storageDirectory); err != nil || len(entries) != 0 {
					t.Fatalf("refused alias collision touched upload storage: %v, %v", entries, err)
				}
				return
			}
			if response.Code != http.StatusCreated {
				t.Fatalf("wire-named multipart create = %d: %s", response.Code, response.Body.String())
			}
			var photo string
			var ready bool
			var quantity int
			if err := database.QueryRow("SELECT photo_raw, is_ready, quantity FROM media").Scan(&photo, &ready, &quantity); err != nil {
				t.Fatal(err)
			}
			if photo == "" || !ready || quantity != 7 {
				t.Fatalf("wire-named fields stored photo=%q ready=%v quantity=%d", photo, ready, quantity)
			}
		})
	}
}

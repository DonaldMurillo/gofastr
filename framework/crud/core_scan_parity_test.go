package crud

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// The pooled scanner List uses on its hook-free path and the buffered one
// every other read uses give the same rows for the same result set.
func TestPooledAndBufferedScannersAgree(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER, name TEXT, done INTEGER, blob BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES (1, 'a', 1, x'6869'), (2, NULL, 0, NULL)`); err != nil {
		t.Fatal(err)
	}
	cols := []string{"id", "name", "done", "blob"}
	keys := []string{"id", "name", "done", "blob"}
	fields := []schema.Field{{Name: "name", Type: schema.String}, {Name: "done", Type: schema.Bool}}
	scan := func(pooled bool) []map[string]any {
		rows, err := db.Query(`SELECT id, name, done, blob FROM t ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if pooled {
			got, err := scanRowsPooledWithKeysForEntity(rows, cols, keys, fields)
			if err != nil {
				t.Fatal(err)
			}
			out := append([]map[string]any(nil), *got...)
			return out
		}
		got, err := scanRowsWithKeysForEntity(rows, cols, keys, fields)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if buffered, pooled := scan(false), scan(true); !reflect.DeepEqual(buffered, pooled) {
		t.Fatalf("buffered %v, pooled %v", buffered, pooled)
	}
}

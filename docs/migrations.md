# Migrations

The core migration runner supports SQL files with directives:

```sql
-- +migrate Version 1
-- +migrate Name create_posts
-- +migrate Up
CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL);
-- +migrate Down
DROP TABLE posts;
```

The CLI loads `migrations/*.sql` in filename order:

```bash
gofastr migrate up --db-url=file:app.db
gofastr migrate status --db-url=file:app.db
gofastr migrate down 1 --db-url=file:app.db
```

SQLite is the default CLI driver. Pass `--driver=<name>` to use another driver
that is linked into the binary.

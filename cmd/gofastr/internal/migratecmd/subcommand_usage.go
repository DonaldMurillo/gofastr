package migratecmd

import (
	"fmt"
)

func printMigrateUsage() {
	fmt.Println(`Usage: gofastr migrate [up|down|status|generate|force|repair] [flags]

Run database migrations. The subcommand defaults to 'up'.

  up       apply pending migrations (default)
  down N   roll back the last N migrations (default 1)
  status   show applied/pending migrations
  generate <name>  emit a reviewable migration from an app spec (--from=<yml>)
  force <V>        mark version V applied without running its SQL (--not-applied removes it)
  repair           report stale owner-column foreign keys on SQLite (--from=<yml>, --apply rebuilds)

Common flags:
  --db-url=<url>   database URL (default: DATABASE_URL, then .env)
  --db=<driver>    sqlite (default), postgres or mysql (aliases: sqlite3, postgresql;
                   --driver=<driver> is the same flag). Postgres and MySQL need a
                   gofastr binary that blank-imports the driver.
  --group=<name>   scope to a migration group (repeatable)
  --create-db      (up) create the database first if it does not exist`)
}

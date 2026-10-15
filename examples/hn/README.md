# hn

A Hacker News reader built the way [HNPWA](https://hnpwa.com/) apps are
built, so its first load can be measured on the same pages as the
Svelte, Nuxt, Angular and other versions.

## What it serves

| Route | Page |
|---|---|
| `/`, `/news` | Top stories, 10 pages |
| `/newest` | Newest first, 12 pages |
| `/ask` | Ask posts, 2 pages |
| `/show` | Show posts, 2 pages |
| `/jobs` | Job posts, 1 page |
| `/item/{id}` | A story and its full comment thread |
| `/user/{id}` | A user's profile |

Lists hold 30 rows and page with `?page=N`. The pager links are plain
`<a href>`, so a page turn works without script. A page past the end, an
unknown item and an unknown user each answer 404.

The data is made up. A fixed-seed generator (`gen.go`) writes 700 users,
390 stories and about 7,300 comments on first boot. Eighteen threads
hold 80 to 300 comments, and the deepest replies sit 14 levels down. Nothing comes
from the network, and no real Hacker News text is copied. Every age is
measured from 2026-01-15 12:00 UTC instead of the clock, so a page
serves the same bytes on every run.

The rows go into SQLite through three entity declarations
(`users`, `stories`, `comments`, CRUD routes off), and each screen's
`Load` reads them back with typed queries. The pages are `framework/ui`
components: `EntryList` for the ranked rows, `CommentThread` for the
nested replies, `Pagination`, `DetailList` and `EmptyState`. The top
bar is the app's own `hnheader` package with an owned style sheet. The
app ships no other CSS.

The page asks for no server-push channel (`uihost.WithNoLiveChannel`),
since nothing on it updates live. There is no theme toggle; the page
follows the OS light or dark setting.

## Run it

```bash
go run ./examples/hn            # http://localhost:8098, or $PORT
```

The database is `hn.db` in the working directory, or the path in
`$HN_DB`. Delete it to reseed.

For numbers, build and run the production binary:

```bash
go build -o dist/examples/hn ./examples/hn
GOFASTR_ENV=production PORT=8098 ./dist/examples/hn
```

In a linked git worktree, also set `GOFASTR_ISOLATION=off`, or the port
can move.

## Benchmark it

Measure the front page and one busy story page. The story with the most
comments in the seeded data is `/item/46106605` (279 comments). With
Lighthouse on its default mobile profile:

```bash
npx lighthouse http://localhost:8098/ --only-categories=performance
npx lighthouse http://localhost:8098/item/46106605 --only-categories=performance
```

Run each page a few times and take the median; one run moves by
several points. Compare against other HNPWA apps on the same page
types: the top list and a story with a long thread. Their data comes
from the live Hacker News API, so their pages differ in size from run
to run.

## Tests

```bash
go test ./examples/hn/...
```

The tests seed a temporary database and drive the router in-process.
They check every list at its first and last page, out-of-range pages,
missing items and users, the nesting depth of the busiest thread, Ask
rows linking to their own page, and the user page.

#!/usr/bin/env bash
# Regenerates the committed pre-upgrade SQLite DB (seed.db) for one fixture.
#
# A fixture ships a frozen v0.38.0 / v0.53.0 generated app pinned to that
# release. seed.db is what booting THAT app once produces: the auth battery's
# auth_users / auth_sessions / auth_users_oauth_links tables, the seeded tags,
# and owner-scoped tasks owned by two real users. The upgrade driver boots the
# UPGRADED app against a copy of this DB to prove migrations apply over real
# existing rows without losing data.
#
# This script is NOT run by CI or the driver — the committed seed.db is the
# source of truth. Run it by hand only when a fixture's generated source or
# blueprint changes and the seed must be re-captured:
#
#     ./evals/upgrade-fixtures/regenerate-seed.sh evals/upgrade-fixtures/fixtures/v0.38.0-app
#     ./evals/upgrade-fixtures/regenerate-seed.sh evals/upgrade-fixtures/fixtures/v0.53.0-app
#
# Requires: Go toolchain, network (the old framework version is fetched from the
# module proxy), and cgo (the old app imports mattn/go-sqlite3).
set -euo pipefail

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <fixture-dir>  (e.g. evals/upgrade-fixtures/fixtures/v0.38.0-app)" >&2
	exit 2
fi

FX="$1"
cd "$FX"

echo "→ building the OLD app at its pinned version ($(sed -n 's/^	github.com\/DonaldMurillo\/gofastr //p' go.mod))"
CGO_ENABLED=1 go build -o /tmp/seed-app .

PORT=127.0.0.1:18099
DB="$(mktemp -d)/seed.db"
rm -f "$DB"
echo "→ booting on $PORT against $DB"
PORT="$PORT" DATABASE_URL="file:$DB" /tmp/seed-app >/tmp/seed-app.log 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT

# Wait for readiness (the home route returns 200 once auto-migrate + bind finish).
for _ in $(seq 1 40); do
	code=$(curl -s -o /dev/null -w '%{http_code}' "http://$PORT/" || true)
	[ "$code" = "200" ] && break
	sleep 0.5
done
A="http://$PORT"

echo "→ seeding: two users + owner-scoped tasks (tags are seeded by the app itself)"
curl -fsS -c /tmp/seed-a.jar -X POST "$A/auth/register" -H 'content-type: application/json' -d '{"email":"alice@ex.com","password":"secret123"}' -o /dev/null
curl -fsS -c /tmp/seed-a.jar -b /tmp/seed-a.jar -X POST "$A/auth/login"    -H 'content-type: application/json' -d '{"email":"alice@ex.com","password":"secret123"}' -o /dev/null
curl -fsS -b /tmp/seed-a.jar -X POST "$A/api/tasks" -H 'content-type: application/json' -d '{"title":"alice task 1"}' -o /dev/null
curl -fsS -b /tmp/seed-a.jar -X POST "$A/api/tasks" -H 'content-type: application/json' -d '{"title":"alice task 2"}' -o /dev/null
curl -fsS -c /tmp/seed-b.jar -X POST "$A/auth/register" -H 'content-type: application/json' -d '{"email":"bob@ex.com","password":"secret123"}' -o /dev/null
curl -fsS -c /tmp/seed-b.jar -b /tmp/seed-b.jar -X POST "$A/auth/login"    -H 'content-type: application/json' -d '{"email":"bob@ex.com","password":"secret123"}' -o /dev/null
curl -fsS -b /tmp/seed-b.jar -X POST "$A/api/tasks" -H 'content-type: application/json' -d '{"title":"bob task 1"}' -o /dev/null

kill "$PID" 2>/dev/null || true
trap - EXIT
sleep 1

cp "$DB" seed.db
rm -f /tmp/seed-a.jar /tmp/seed-b.jar /tmp/seed-app
echo "→ wrote $FX/seed.db ($(wc -c < seed.db) bytes)"
echo "  auth_users=$(sqlite3 seed.db 'select count(*) from auth_users' 2>/dev/null || echo '?')  tasks=$(sqlite3 seed.db 'select count(*) from tasks' 2>/dev/null || echo '?')  tags=$(sqlite3 seed.db 'select count(*) from tags' 2>/dev/null || echo '?')"

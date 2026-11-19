# Replay suite after the experimental and core sweeps, 2026-11-19

Opus, `harness` condition, all replay tasks at main after the sweeps
(`548d9fa5f`) and before them (`80d23dd00`), two runs per task, ten for
`node-block-entity`. Six cells that errored on a stale `index.lock` were
run again with `GOFLAGS=-buildvcs=false`; the cause, `gofastr dev` running
git in the repository, is fixed on this branch.

## 1. Is the eval working?

**Partly**: every cell graded, but no replay task reads `core/` or
`framework/experimental`, so both commits gave the agent the same
entries, and this run cannot see the sweep (48 cells, $53).

## 2. What is working, and how much

| Opus, harness | before | after |
|---|---|---|
| Reported path fixed | 24/24 | 24/24 |
| Other paths fixed, `node-block-entity`, ten runs | 38/40 | 32/40 |
| Other paths fixed, the other seven tasks | 28/38 | 29/38 |
| Updated the docs | 22/24 | 24/24 |

## 3. What I think

The `node-block-entity` drop is noise: both sides ran the same inputs,
and the last four runs went 14/16 against 16/16. The sweeps lost nothing
the suite can measure. I would change my mind if a replay task cut from a
`core/` fix scored lower after the sweep.

## 4. Next steps

- Add a replay task cut from a real `core/router` or `core/mcp` fix.
- Find why the harness's deleted cell files hold disk until it exits.

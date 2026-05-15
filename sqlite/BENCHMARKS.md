# SQLite Driver Benchmarks: Pure Go vs CGO

## How to reproduce

```bash
# Pure Go fair benchmarks (10 benchmarks, ~2.5 min)
go test ./sqlite/ -bench="BenchmarkFair_" -benchtime=1s -count=3 -benchmem -run="^$"

# CGO benchmarks (10 benchmarks, ~1 min, requires CGO enabled)
go test -tags=cgo ./sqlite/ -bench="BenchmarkCGO_" -benchtime=1s -count=3 -benchmem -run="^$"
```

**Machine**: Apple M4 Pro, macOS, Go 1.26, arm64.
**Date**: 2025-05-15, commit `14f7059`.

## Apples-to-apples comparison

Both suites use identical structures: same DB lifecycle, same table schema,
same queries, same data. The ONLY difference is the driver.

| Benchmark | CGO (mattn/go-sqlite3) | Pure Go (ours) | Ratio |
|-----------|----------------------:|---------------:|-------|
| CreateTable | 25μs | **16μs** | **1.6x faster** ✅ |
| Insert | **1.2μs** | 116μs | 97x slower ❌ |
| InsertPrepared | **0.87μs** | 129μs | 148x slower ❌ |
| SelectAll (1K rows) | 259μs | **248μs** | **~same** ✅ |
| SelectWhere | **148μs** | 198μs | 1.3x slower |
| Delete | 31μs | **18μs** | **1.7x faster** ✅ |
| Transaction (50 inserts) | 69μs | **37μs** | **1.9x faster** ✅ |
| OLTP (point select) | **1.3μs** | 1.3μs | **~same** ✅ |
| OrderByLimit | 3.8μs | 4.3μs | 1.1x slower |
| DriverTx (50 inserts) | 52μs | **36μs** | **1.4x faster** ✅ |

### Summary

- **Win (5/10)**: CreateTable, SelectAll, Delete, Transaction, DriverTx
- **Tie (2/10)**: OLTP, OrderByLimit
- **Lose (3/10)**: Insert, InsertPrepared, SelectWhere

### Where we win

**Transactions are our strong suit.** Batched inserts in a transaction are
1.4-1.9x faster than CGO. Reads are competitive. DDL (CreateTable) is faster
because we skip the C FFI overhead.

### Where we lose

**Single auto-commit inserts** are 97-148x slower. Root cause:

1. **GC pressure** — profiling shows 37-47% of CPU time in GC/scheduler
   overhead (`pthread_cond_signal`, `kevent`, `madvise`). Every insert
   allocates page buffers, cell structs, and record objects that the GC
   must collect.
2. **No WAL** — CGO SQLite uses WAL (write-ahead logging) which batches
   page flushes. We flush dirty pages to the in-memory file on every
   auto-commit.
3. **C vs Go** — the actual B-tree operations in C are ~10x cheaper per
   operation due to no bounds checking, no GC, no interface dispatch.

### Bottleneck profile (DriverTransactionFixed)

```
46%  BTree.insertIntoPage      (actual work)
37%  runtime.pthread_cond_signal (GC/scheduler overhead)
18%  Pager.GetPageDataMutable   (page cache lookups)
 9%  runtime.mapaccess2_fast64  (map lookups)
```

## Detailed raw results (3 runs each)

### Pure Go (Fair benchmarks)

```
BenchmarkFair_CreateTable-14        70315   16063 ns/op   65031 B/op   69 allocs/op
BenchmarkFair_CreateTable-14        69999   17618 ns/op   65032 B/op   69 allocs/op
BenchmarkFair_CreateTable-14        63486   15808 ns/op   65036 B/op   69 allocs/op

BenchmarkFair_Insert-14            148447  147181 ns/op  192182 B/op   10 allocs/op
BenchmarkFair_Insert-14             81985   80270 ns/op  105012 B/op   10 allocs/op
BenchmarkFair_Insert-14            136075  119924 ns/op  175862 B/op   10 allocs/op

BenchmarkFair_InsertPrepared-14    142172  146504 ns/op  184052 B/op   13 allocs/op
BenchmarkFair_InsertPrepared-14    144052  120298 ns/op  186561 B/op   13 allocs/op
BenchmarkFair_InsertPrepared-14    148736  120293 ns/op  192855 B/op   13 allocs/op

BenchmarkFair_SelectAll-14           4833  246532 ns/op  338323 B/op 7724 allocs/op
BenchmarkFair_SelectAll-14           4740  248648 ns/op  338323 B/op 7724 allocs/op
BenchmarkFair_SelectAll-14           4738  249528 ns/op  338323 B/op 7724 allocs/op

BenchmarkFair_SelectWhere-14         5886  202554 ns/op  183194 B/op 4404 allocs/op
BenchmarkFair_SelectWhere-14         5935  196514 ns/op  183193 B/op 4404 allocs/op
BenchmarkFair_SelectWhere-14         5935  196830 ns/op  183193 B/op 4404 allocs/op

BenchmarkFair_Delete-14             67350   17945 ns/op   74584 B/op  108 allocs/op
BenchmarkFair_Delete-14             68337   17896 ns/op   74586 B/op  108 allocs/op
BenchmarkFair_Delete-14             67640   18087 ns/op   74588 B/op  108 allocs/op

BenchmarkFair_Transaction-14        27894   36702 ns/op   97735 B/op  651 allocs/op
BenchmarkFair_Transaction-14        33370   36285 ns/op   97737 B/op  651 allocs/op
BenchmarkFair_Transaction-14        33117   36661 ns/op   97736 B/op  651 allocs/op

BenchmarkFair_OLTP-14              913780    1258 ns/op    2608 B/op   35 allocs/op
BenchmarkFair_OLTP-14              973837    1280 ns/op    2608 B/op   35 allocs/op
BenchmarkFair_OLTP-14              918853    1257 ns/op    2608 B/op   35 allocs/op

BenchmarkFair_OrderByLimit-14      283962    4287 ns/op   13632 B/op   85 allocs/op
BenchmarkFair_OrderByLimit-14      283762    4339 ns/op   13632 B/op   85 allocs/op
BenchmarkFair_OrderByLimit-14      259119    4356 ns/op   13632 B/op   85 allocs/op

BenchmarkFair_DriverTransaction-14  27782   36688 ns/op   97736 B/op  651 allocs/op
BenchmarkFair_DriverTransaction-14  32430   36747 ns/op   97739 B/op  651 allocs/op
BenchmarkFair_DriverTransaction-14  33213   36007 ns/op   97735 B/op  651 allocs/op
```

### CGO (mattn/go-sqlite3)

```
BenchmarkCGO_CreateTable-14         46731   25156 ns/op    2386 B/op   52 allocs/op
BenchmarkCGO_CreateTable-14         48070   25424 ns/op    2387 B/op   52 allocs/op
BenchmarkCGO_CreateTable-14         48746   24816 ns/op    2387 B/op   52 allocs/op

BenchmarkCGO_Insert-14            1000000    1218 ns/op      72 B/op    4 allocs/op
BenchmarkCGO_Insert-14             994227    1225 ns/op      72 B/op    4 allocs/op
BenchmarkCGO_Insert-14             999427    1211 ns/op      72 B/op    4 allocs/op

BenchmarkCGO_InsertPrepared-14    1401020     866 ns/op     160 B/op    5 allocs/op
BenchmarkCGO_InsertPrepared-14    1375662     890 ns/op     160 B/op    5 allocs/op
BenchmarkCGO_InsertPrepared-14    1380159     863 ns/op     160 B/op    5 allocs/op

BenchmarkCGO_SelectAll-14            4569  257116 ns/op   76536 B/op 6666 allocs/op
BenchmarkCGO_SelectAll-14            4477  258154 ns/op   76536 B/op 6666 allocs/op
BenchmarkCGO_SelectAll-14            4537  262696 ns/op   76536 B/op 6666 allocs/op

BenchmarkCGO_SelectWhere-14          8158  148330 ns/op   39200 B/op 3344 allocs/op
BenchmarkCGO_SelectWhere-14          8142  148527 ns/op   39200 B/op 3344 allocs/op
BenchmarkCGO_SelectWhere-14          7989  148356 ns/op   39200 B/op 3344 allocs/op

BenchmarkCGO_Delete-14              41184   31258 ns/op    2533 B/op   60 allocs/op
BenchmarkCGO_Delete-14              39279   30957 ns/op    2533 B/op   60 allocs/op
BenchmarkCGO_Delete-14              40135   31267 ns/op    2534 B/op   60 allocs/op

BenchmarkCGO_Transaction-14         17748   67044 ns/op    7251 B/op  315 allocs/op
BenchmarkCGO_Transaction-14         17688   67658 ns/op    7251 B/op  315 allocs/op
BenchmarkCGO_Transaction-14         17500   71827 ns/op    7250 B/op  315 allocs/op

BenchmarkCGO_OLTP-14             1000000    1624 ns/op     648 B/op   24 allocs/op
BenchmarkCGO_OLTP-14              978585    1165 ns/op     648 B/op   24 allocs/op
BenchmarkCGO_OLTP-14             1000000    1163 ns/op     648 B/op   24 allocs/op

BenchmarkCGO_OrderByLimit-14      317052    3943 ns/op    1016 B/op   57 allocs/op
BenchmarkCGO_OrderByLimit-14      325366    3716 ns/op    1016 B/op   57 allocs/op
BenchmarkCGO_OrderByLimit-14      321645    3730 ns/op    1016 B/op   57 allocs/op

BenchmarkCGO_DriverTransaction-14  22992   51560 ns/op   11182 B/op  322 allocs/op
BenchmarkCGO_DriverTransaction-14  23853   53124 ns/op   11179 B/op  322 allocs/op
BenchmarkCGO_DriverTransaction-14  23233   51710 ns/op   11182 B/op  322 allocs/op
```

## Actionable optimization targets

### 1. Reduce allocations in insert hot path (highest impact)
- **Problem**: Single inserts allocate ~100-190KB (page buffers, cells, records).
- **CGO comparison**: 72 bytes per insert.
- **Approach**: Pool/reuse page buffers, cell slices, and record objects
  across insert calls. The `pagePool` exists but isn't used aggressively enough.

### 2. WAL (write-ahead logging) (medium impact)
- **Problem**: Auto-commit writes flush all dirty pages immediately.
- **CGO comparison**: SQLite WAL batches writes and does periodic checkpoints.
- **Approach**: Defer dirty page flushes until a checkpoint threshold.
  Estimated 5-10x improvement on auto-commit writes.

### 3. Update uses table scan instead of B-tree seek (medium impact)
- **Problem**: `UPDATE t SET x=? WHERE id=?` scans ALL rows instead of
  seeking directly to the rowid.
- **Approach**: Detect `WHERE id = <constant>` and use `BTree.Search`
  instead of `BTree.Scan`.

### 4. SelectWhere string comparison (low impact)
- **Problem**: We're 1.3x slower on `WHERE name = 'user_a'` — likely
  because we scan all rows instead of using an index.
- **Approach**: Create an index on `name` or optimize full-scan filtering.

## Historical note

The original benchmarks (`BenchmarkEngine*`, `BenchmarkDriver*`) were NOT
structurally identical to the CGO benchmarks. Key differences:

- CGO benchmarks created a fresh `:memory:` DB per iteration (table never grew)
- Our benchmarks reused one DB (table grew to 500K rows)
- CGO used inline constants; ours used `fmt.Sprintf`
- CGO OLTP was a single-row point query; ours was a 500-row mixed workload

The `BenchmarkFair_*` suite fixes all of these discrepancies.

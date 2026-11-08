# Pack round-trip correctness review — November 7, 2026

Reviewed revision: `953ccc08256e825bdc17254c463a5695a27092b6`.

## C11 — P2: packed data-table rows are strings, making the snapshot invalid

`cmd/gofastr/internal/packcmd/pack.go:752` handles map list items but not
nested list items. `writeScalarInline` falls through to `fmt.Sprint` for a
`[]any` row (`pack.go:880`), converting e.g. `[Ada, Admin]` into the string
`"[Ada Admin]"`. The reverse table reader already recovered a list of rows;
serialization changes each row's type.

Public path: validate a supported spec containing a data table with rows →
generate an app → pack that app → validate/regenerate the packed YAML.
The initial spec is valid and generation/pack return success, but the packed
spec fails because `rows[0]` is not a list. This is not just lossy extraction:
the serializer emits a value its own schema cannot read. The serializer's
round-trip invariant is explicit at `pack.go:42`.

The reviewer also proposed omitted island/widget/section constructs. Those
omissions are not counted here: pack explicitly describes itself as lossy and
warns about affected files. No separate established guarantee was demonstrated.

### Independent public-CLI fixture

Save the following to a fresh temporary directory as `gofastr.yml`:

```yaml
app:
  name: CoordinatorTable
  module: example.com/coordinator-table
  db:
    driver: sqlite
    url: file:coordinator-table.db
entities:
  - name: widgets
    crud: true
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: roster
    route: /roster
    title: Roster
    body:
      - kind: data_table
        props:
          caption: People
          columns: [Name, Role]
          rows:
            - [Ada, Admin]
            - [Grace, Developer]
```

From the checkout root build the actual CLI with an explicit temporary output
path; run every generation/pack operation in the fixture directory, not the
checkout:

```sh
go build -p 2 -o /tmp/gofastr-coordinator-pack/gofastr ./cmd/gofastr
cd /tmp/gofastr-coordinator-pack
./gofastr validate gofastr.yml
./gofastr generate app gofastr.yml --out=generated --force
./gofastr pack generated -o packed.yml
./gofastr validate packed.yml
./gofastr generate app packed.yml --out=regenerated --force
```

The final two commands reproduce the failure; they are not expected to pass
until this defect is fixed. No generated project dependencies need installation.
No production source fix is included.

### Independently executed results

- Fresh CLI built from the reviewed checkout to `/tmp/gofastr-coordinator-pack/gofastr`.
- Initial validation, generation and pack all succeed. Packed rows are
  `"[Ada Admin]"` and `"[Grace Developer]"`, not nested lists.
- Packed validation and regeneration both refuse `rows[0]: row must be a list`.
- Control: the same table shape with the `rows` property omitted validates,
  generates, packs, validates again and regenerates successfully.
- Full `cmd/gofastr/internal/packcmd` suite passes despite the reproduced defect.
- No generated project was built or served; warnings about initializing its
  Go module are unrelated to the observed spec serialization failure.

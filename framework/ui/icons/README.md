# GoFastr icon catalog

This package exposes 291 drawings: all 287 icons from
[Feather](https://github.com/feathericons/feather) and four from
[Lucide](https://github.com/lucide-icons/lucide). Thirteen aliases preserve
existing names. The SVGs in `svg/` are byte-for-byte upstream copies. The old
inline registry has been replaced by this catalog; the site's header, search
and GitHub glyphs use it too.

| Source | Pinned commit | License | Retrieval |
| --- | --- | --- | --- |
| Feather | [`3dc050d97405062eba78aa57115c0a15c63abdaa`](https://github.com/feathericons/feather/commit/3dc050d97405062eba78aa57115c0a15c63abdaa) | [LICENSE.feather](LICENSE.feather), MIT, Copyright (c) 2013-2023 Cole Bemis | Initial 20 files: October 24, 2026, 22:26:54 UTC. Remaining files: October 25, 2026, 05:59:29 UTC. |
| Lucide | [`7faf87628a1ad6175975bc595344337ce14a7db4`](https://github.com/lucide-icons/lucide/commit/7faf87628a1ad6175975bc595344337ce14a7db4) | [LICENSE.lucide](LICENSE.lucide), ISC with the upstream MIT notice retained | October 25, 2026, 05:59:29 UTC. |

Lucide supplies `chevrons-up-down`, `receipt`, `flame`, and `diamond`.
[sources.json](sources.json), schema version 2, records each icon's source ID,
actual retrieval timestamp, original path, pinned download URL, local path,
SHA-256 and modifications. Each upstream has its own commit and license record.

The SVG bytes have no modifications. Local filename mappings include `close`
to `x`, `danger` to `alert-octagon`, `edit` to `edit-2`, `settings` to `sliders`,
`trash` to `trash-2`, and `warning` to `alert-triangle`. The manifest records
every mapping. The upstream `edit`, `settings`, and `trash` variants are
available as `edit-square`, `settings-gear`, and `trash-simple`, preserving
the first 20 local names.

`icons.Names()` lists canonical drawings. `icons.Aliases()` returns a copy of
the alternate-name map. Both sets have generated string constants. Aliases
do not duplicate SVG files. The gallery searches aliases and shows the
canonical name on each tile.

`icons.Categories()` returns 19 curated groups with stable `ID` and display
`Label` fields. `icons.CategoryFor(name)` returns the drawing's category and a
boolean, resolving aliases first. Unknown names return false. Both APIs return
caller-owned values. Every drawing has exactly one category in `categories.go`;
this local metadata does not change the upstream SVGs or provenance manifest.

The `/components/icon` browser has a single-row sticky search bar with a Filters
dropdown and category counts. The result count scrolls above the bar. Each
tile shows one centered 24px icon and its name. The whole tile copies the name
and briefly shows a copied confirmation. Categories and aliases remain
searchable without adding text to the tiles. The grid uses a 5rem minimum.
Search ignores case and matches all
entered words in names and aliases, or in a category label. Hyphens and
underscores count as spaces. Search and category combine; Search submits the
GET form, and Clear removes both. The `q` and `category` URL parameters preserve
the view across refresh, sharing, and browser history.
With JavaScript, submissions use client navigation and keep the current page
document loaded. Without JavaScript the same GET form works natively.

| Alternate name | Canonical drawing |
| --- | --- |
| `alert-octagon` | `danger` |
| `alert-triangle` | `warning` |
| `card` | `credit-card` |
| `chart` | `bar-chart` |
| `edit-2`, `pencil` | `edit` |
| `fire` | `flame` |
| `more` | `more-horizontal` |
| `panel-left` | `sidebar` |
| `sliders` | `settings` |
| `success` | `check` |
| `trash-2` | `trash` |
| `x` | `close` |

The generator extracts the inner geometry and normalizes XML serialization.
It does not edit coordinates. `ui.Icon` supplies the SVG wrapper, size, color,
and accessibility attributes. Neither generation nor rendering fetches assets
from the network.

Keep the upstream copyright and full permission notices with redistributed
artwork or generated drawing data, including applications that bundle them.
The complete notices are in [LICENSE.feather](LICENSE.feather) and
[LICENSE.lucide](LICENSE.lucide), in the header of
`icons_gen.go`, and in the repository's
[third-party notices](../../../THIRD_PARTY_NOTICES.md). GoFastr's own license
does not replace the upstream notice.

## Updating the vendored set

1. Select a full upstream commit SHA. Download the chosen SVGs and `LICENSE`
   using URLs pinned to that commit, and retain their exact bytes.
2. Record each upstream under `sources`, including its full commit and license
   hash. Record every icon's source ID, actual UTC `retrieved_at`, upstream and
   local paths, pinned URL and SHA-256. Keep `modifications` empty for unmodified
   SVGs. Record aliases separately; targets must be canonical, with no shadowing.
3. Update the source record above and the repository's third-party notices.
   Include the full license text from the selected commit.
   Assign each new canonical drawing to a group in `categories.go`.
4. Run generation and tests. Generation refuses changed checksums, incomplete
   provenance, unpinned URLs, missing source records, invalid aliases, and
   unsupported SVG markup. It also rejects names that produce the same Go
   identifier as another icon or a handwritten package declaration, before
   replacing the generated file.

The SVGs use a 24 by 24 grid, a two-unit rounded stroke, and `currentColor`.
Do not redraw the vendored files. Custom app artwork can use `ui.RegisterIcon`
with its own documented provenance.

From the repository root:

```sh
go generate ./framework/ui/icons
go test ./framework/ui/icons/... ./framework/ui ./framework/gallery -short
```

Review `/components/icon` in both color schemes and on phones.
Check the navigation and link examples as well as the individual drawings.
Commit the manifest, SVGs, license, notices, and generated Go data together.

The source-level coverage test checks literal icon references in the framework,
gallery, site and embedded app specs. It also refuses private site icon
registrations and inline site SVGs. Validate dynamic names through
`ui.IconRegistered`. Charts, app artwork and filled rating shapes remain
separate component drawing code.

The public API and accessibility guidance are in the
[icon reference](../../docs/content/ui-new-components.md#icons).

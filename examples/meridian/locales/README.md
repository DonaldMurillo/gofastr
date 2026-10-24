<!-- gofastr:generated generator=app kind=scaffold version=dev sum=0c4ebb4e4d56793e -->
# Translations

Add one JSON file per language, named by its tag: `es.json`,
`pt-br.json`. Each key is a phrase key. The screens declare theirs at
the top of each screen file (`i18n.Define("<key>", "<English>")`).
`i18n.Phrases()` returns every key the app knows with its English,
the framework's own words (form buttons, table headers, the admin)
included. Dots nest:

```json
{
  "shop": {"screens": {"home": {"welcome": "Bienvenido"}}},
  "entity": {"orders": {"name": "Pedidos"}}
}
```

A key with no translation renders its English. Each language lives under its own
path: `/es/` serves es.json, and English keeps the plain paths. Run
with `GOFASTR_PSEUDO_LOCALE=1` and open `/en-xa/` to see every phrase
accented and lengthened.

<!-- gofastr:generated generator=app kind=scaffold version=dev sum=62e006b552c9ed22 -->
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

A key with no translation renders its English. The browser's
Accept-Language picks the locale. Run with `GOFASTR_PSEUDO_LOCALE=1` and
ask for `en-XA` to see every phrase accented and lengthened.

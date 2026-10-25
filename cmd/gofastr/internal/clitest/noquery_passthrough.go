package clitest

// AppSpecWithNoQuery is a minimal app whose one entity has a masked column,
// used to drive the generator end to end. No fixture in the repo declares
// no_query, so without this the whole emission path was covered only by unit
// tests on individual render functions.
const AppSpecWithNoQuery = `
app:
  name: cards
entities:
  - name: cards
    fields:
      - name: label
        type: string
      - name: number
        type: string
        no_query: true
`

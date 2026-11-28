package clitest

// AppSpecWithNoQuery is an app spec whose one entity has a no_query column.
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

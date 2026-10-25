package clitest

const OwnerUniqueYML = `
app:
  name: Demo
entities:
  - name: customers
    owner_field: user_id
    fields:
      - name: email
        type: string
        unique: true
`

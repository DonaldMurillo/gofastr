package clitest

// --- gofastr validate: unscoped PII is an error (exit 1).

const PiiTripYML = `
app:
  name: Demo
entities:
  - name: patients
    fields:
      - name: email
        type: string
      - name: ssn
        type: string
`

const PiiOwnerYML = `
app:
  name: Demo
entities:
  - name: patients
    owner_field: user_id
    fields:
      - name: email
        type: string
      - name: user_id
        type: string
`

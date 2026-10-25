package clitest

// CompositeScreensSpec gives a composite-key entity the screens an app
// spec hangs off a record: a list with a create form, a detail screen
// and an edit form, both at {id}.
const CompositeScreensSpec = `app:
  name: Roster
  module: example.com/roster
  db:
    driver: sqlite
    url: file:roster.db
  auth:
    enabled: true
  admin:
    enabled: true
    seed_email: admin@example.com
    seed_password: roster-pw-123
entities:
  - name: enrollments
    crud: true
    primary_key: [student_id, course_id]
    fields:
      - name: student_id
        type: string
        required: true
      # The generated e2e test creates with the first value: a comma in a
      # key value must survive reading the record's id back.
      - name: course_id
        type: enum
        values: ["c,1", "c 1"]
        required: true
      - name: grade
        type: string
        required: true
screens:
  - name: home
    route: /
    title: Roster
    body:
      - type: heading
        level: 1
        text: Roster
  - name: enrollments
    route: /enrollments
    access:
      auth: true
    body:
      - type: entity_list
        entity: enrollments
        fields: [student_id, course_id, grade]
        create: true
  - name: enrollment
    route: /enrollments/{id}
    access:
      auth: true
    body:
      - type: entity_detail
        entity: enrollments
  # A param before the record: the base Save returns to escapes it.
  - name: enrollment_edit
    route: /{term}/enrollments/{id}/edit
    access:
      auth: true
    body:
      - type: entity_form
        entity: enrollments
        mode: edit
`

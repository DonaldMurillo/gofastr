package clitest

func AccessAppSpecYAML() string {
	return `
app:
  name: AccessDemo
  module: example.com/accessdemo
  db:
    driver: sqlite
    url: file:demo.db
entities:
  - name: posts
    owner_field: user_id
    access:
      read: posts:read
      create: posts:write
      update: posts:write
      delete: posts:admin
    fields:
      - name: title
        type: string
        required: true
      - name: user_id
        type: string
`
}

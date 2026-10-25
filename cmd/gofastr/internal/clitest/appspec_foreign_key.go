package clitest

// OrderLinesYAML: lines belong to an order keyed on (store, no) through a
// two-column foreign key.
const OrderLinesYAML = `  - name: orders
    primary_key: [store, no]
    fields:
      - name: store
        type: string
        required: true
      - name: no
        type: int
        required: true
  - name: lines
    fields:
      - name: order_store
        type: string
      - name: order_no
        type: int
      - name: sku
        type: string
    relations:
      - type: belongs_to
        name: order
        entity: orders
        foreign_key: [order_store, order_no]
`

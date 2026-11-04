package review

type pool struct{}
type node struct{ value int }
type holder struct {
	child       *node
	left, right node
}

func (*pool) next() (int, bool, error)    { return 0, false, nil }
func (*pool) child() (*node, bool, error) { return nil, false, nil }

func increment(source *pool) {
	count, _, _ := source.next() // want `assignment discards the error from next`
	count++
}

func compound(source *pool) {
	count, _, _ := source.next() // want `assignment discards the error from next`
	count += 1
}

func fieldIncrement(source *pool, target *node) {
	target.value, _, _ = source.next() // want `assignment discards the error from next`
	target.value--
}

func childWrite(source *pool, target *holder) {
	target.child, _, _ = source.child() // want `assignment discards the error from child`
	target.child.value = 1
}

func indexedRead(source *pool, items []node, index int) int {
	items[index].value, _, _ = source.next() // want `assignment discards the error from next`
	return items[index].value
}

func dereferencedRead(source *pool, target *node) int {
	(*target).value, _, _ = source.next() // want `assignment discards the error from next`
	return (*target).value
}

func siblingRead(source *pool, target *holder) int {
	target.left.value, _, _ = source.next()
	return target.right.value
}

func sameNestedRead(source *pool, target *holder) int {
	target.left.value, _, _ = source.next() // want `assignment discards the error from next`
	return target.left.value
}

func differentIndex(source *pool, items []node, first, second int) int {
	items[first].value, _, _ = source.next()
	return items[second].value
}

func overwritten(source *pool, target *node) {
	target.value, _, _ = source.next()
	target.value = 1
}

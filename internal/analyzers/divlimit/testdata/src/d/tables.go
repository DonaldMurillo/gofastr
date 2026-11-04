package d

var handlerTable = map[string]func(int, int) int{
	"pages": func(total, limit int) int {
		return total / limit // want `integer division by limit`
	},
	"guarded": func(total, limit int) int {
		if limit == 0 {
			return 0
		}
		return total / limit
	},
}

var handlerSlice = []func(int, int) int{
	func(total, limit int) int {
		return total % limit // want `integer division by limit`
	},
}

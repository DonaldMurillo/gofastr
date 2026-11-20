package uinodev1

// Limits are the whole-tree caps; a zero field takes its default.
type Limits struct {
	// MaxDepth bounds nesting depth, the root at 1.
	MaxDepth int

	// MaxNodes bounds the number of nodes.
	MaxNodes int

	// MaxChildrenPerNode bounds one node's children.
	MaxChildrenPerNode int

	// MaxPropItems bounds each props array.
	MaxPropItems int

	// MaxPropString bounds each prop string, in bytes.
	MaxPropString int

	// MaxTotalText bounds the bytes of every string in the tree together.
	MaxTotalText int

	// MaxInputBytes bounds the raw JSON input.
	MaxInputBytes int

	// MaxActionRefLen bounds an ActionRef.
	MaxActionRefLen int
}

const (
	DefaultMaxDepth           = 32
	DefaultMaxNodes           = 500
	DefaultMaxChildrenPerNode = 128
	DefaultMaxPropItems       = 128
	DefaultMaxPropString      = 4 * 1024
	DefaultMaxTotalText       = 256 * 1024
	DefaultMaxInputBytes      = 1 << 20
	DefaultMaxActionRefLen    = 128
)

// DefaultLimits returns the default caps.
func DefaultLimits() Limits {
	return Limits{
		MaxDepth:           DefaultMaxDepth,
		MaxNodes:           DefaultMaxNodes,
		MaxChildrenPerNode: DefaultMaxChildrenPerNode,
		MaxPropItems:       DefaultMaxPropItems,
		MaxPropString:      DefaultMaxPropString,
		MaxTotalText:       DefaultMaxTotalText,
		MaxInputBytes:      DefaultMaxInputBytes,
		MaxActionRefLen:    DefaultMaxActionRefLen,
	}
}

func (l Limits) withDefaults() Limits {
	if l.MaxDepth == 0 {
		l.MaxDepth = DefaultMaxDepth
	}
	if l.MaxNodes == 0 {
		l.MaxNodes = DefaultMaxNodes
	}
	if l.MaxChildrenPerNode == 0 {
		l.MaxChildrenPerNode = DefaultMaxChildrenPerNode
	}
	if l.MaxPropItems == 0 {
		l.MaxPropItems = DefaultMaxPropItems
	}
	if l.MaxPropString == 0 {
		l.MaxPropString = DefaultMaxPropString
	}
	if l.MaxTotalText == 0 {
		l.MaxTotalText = DefaultMaxTotalText
	}
	if l.MaxInputBytes == 0 {
		l.MaxInputBytes = DefaultMaxInputBytes
	}
	if l.MaxActionRefLen == 0 {
		l.MaxActionRefLen = DefaultMaxActionRefLen
	}
	return l
}

const maxErrInputLen = 100

package uinodev1

// Limits are the whole-tree fail-closed caps (design §9). All caps are
// enforced BEFORE the validator walks the tree (for input bytes) and
// DURING the walk (for depth / nodes / children / per-prop strings /
// total text). On any overflow, Validate rejects the whole tree — it
// never truncates.
//
// The zero-value Limits uses [DefaultLimits] for every field that is
// unset (zero), so callers may set only the caps they want to override.
// Pass [DefaultLimits] explicitly to get the documented defaults.
type Limits struct {
	// MaxDepth bounds the tree's nesting depth (root = depth 1).
	// Default 32.
	MaxDepth int

	// MaxNodes bounds the total number of nodes in the tree.
	// Default 500.
	MaxNodes int

	// MaxChildrenPerNode bounds the number of children a single node
	// may have. Default 128.
	MaxChildrenPerNode int

	// MaxPropString bounds the length (bytes) of any single string
	// field in any prop struct. Default 4096 (4 KiB).
	MaxPropString int

	// MaxTotalText bounds the sum of every string field across the
	// whole tree (a conservative upper bound; the actual rendered text
	// is smaller because not every field becomes visible text).
	// Default 262144 (256 KiB).
	MaxTotalText int

	// MaxInputBytes bounds the raw JSON input length. Default 1048576
	// (1 MiB). Callers receiving the tree from an unbounded source MUST
	// ALSO cap the source (e.g. io.LimitReader) so a hostile publisher
	// cannot exhaust memory before Validate runs.
	MaxInputBytes int

	// MaxActionRefLen bounds the length of an ActionRef string.
	// Default 128.
	MaxActionRefLen int
}

// Default caps (design §9). These are intentionally tight: a module
// screen is bounded; if a real screen needs more, the host can pass a
// higher Limits value, but the defaults fail closed.
const (
	DefaultMaxDepth           = 32
	DefaultMaxNodes           = 500
	DefaultMaxChildrenPerNode = 128
	DefaultMaxPropString      = 4 * 1024   // 4 KiB
	DefaultMaxTotalText       = 256 * 1024 // 256 KiB
	DefaultMaxInputBytes      = 1 << 20    // 1 MiB
	DefaultMaxActionRefLen    = 128
)

// DefaultLimits returns the documented default caps. Validate applies
// these automatically when given a zero-value Limits; callers only need
// to call this explicitly when they want to inspect or fork the defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxDepth:           DefaultMaxDepth,
		MaxNodes:           DefaultMaxNodes,
		MaxChildrenPerNode: DefaultMaxChildrenPerNode,
		MaxPropString:      DefaultMaxPropString,
		MaxTotalText:       DefaultMaxTotalText,
		MaxInputBytes:      DefaultMaxInputBytes,
		MaxActionRefLen:    DefaultMaxActionRefLen,
	}
}

// withDefaults returns a copy of l with every zero field replaced by
// its documented default. Non-zero fields are preserved, so callers may
// override individual caps.
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

// maxErrInputLen caps how much of any attacker-controlled string is
// echoed back in an error message. Errors must say WHAT failed without
// becoming an amplification channel for the attacker's payload.
const maxErrInputLen = 100

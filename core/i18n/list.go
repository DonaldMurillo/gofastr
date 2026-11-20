package i18n

import "context"

// List joins items as ctx's locale writes "a, b, and c".
func List(ctx context.Context, items []string) string {
	return joinList(cldrFor(ctx).loc.and, items)
}

// ListOr joins items as ctx's locale writes "a, b, or c".
func ListOr(ctx context.Context, items []string) string {
	return joinList(cldrFor(ctx).loc.or, items)
}

func joinList(p [4]string, items []string) string {
	switch n := len(items); n {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return joinPattern(p[3], items[0], items[1], false)
	default:
		s := joinPattern(p[2], items[n-2], items[n-1], false)
		for i := n - 3; i >= 1; i-- {
			s = joinPattern(p[1], items[i], s, false)
		}
		return joinPattern(p[0], items[0], s, false)
	}
}

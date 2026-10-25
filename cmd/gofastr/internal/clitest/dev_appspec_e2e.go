package clitest

func Clip(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "…"
	}
	return s
}

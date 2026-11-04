package a

import "regexp"

func scrubReassignedPattern(input string) string {
	pattern := regexp.MustCompile(`[x]`)
	pattern = regexp.MustCompile(`../`)
	return pattern.ReplaceAllString(input, "") // want `sanitizefix: scrubReassignedPattern removes a multi-character match in one pass`
}

func scrubConditionallyReassignedPattern(input string, usePath bool) string {
	pattern, err := regexp.Compile(`[x]`)
	if err != nil {
		return ""
	}
	if usePath {
		pattern = regexp.MustCompile(`../`)
	}
	return pattern.ReplaceAllString(input, "") // want `sanitizefix: scrubConditionallyReassignedPattern removes a multi-character match in one pass`
}

func scrubStableLocalPattern(input string) string {
	pattern := regexp.MustCompile(`[x]`)
	return pattern.ReplaceAllString(input, "")
}

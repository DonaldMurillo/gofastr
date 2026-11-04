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

func scrubRangeReassigned(input string, candidates []*regexp.Regexp) string {
	pattern := regexp.MustCompile(`[x]`)
	for _, pattern = range candidates {
		input = pattern.ReplaceAllString(input, "") // want `sanitizefix: scrubRangeReassigned removes a multi-character match in one pass`
	}
	return input
}

func scrubRangeKeyReassigned(input string, candidates map[*regexp.Regexp]bool) string {
	pattern := regexp.MustCompile(`[x]`)
	for pattern = range candidates {
		input = pattern.ReplaceAllString(input, "") // want `sanitizefix: scrubRangeKeyReassigned removes a multi-character match in one pass`
	}
	return input
}

func scrubBeforeLaterWrite(input string) string {
	pattern := regexp.MustCompile(`[x]`)
	clean := pattern.ReplaceAllString(input, "")
	pattern = regexp.MustCompile(`../`)
	return pattern.ReplaceAllString(clean, "") // want `sanitizefix: scrubBeforeLaterWrite removes a multi-character match in one pass`
}

func scrubBeforeLaterRange(input string, candidates []*regexp.Regexp) string {
	pattern := regexp.MustCompile(`[x]`)
	clean := pattern.ReplaceAllString(input, "")
	for _, pattern = range candidates {
		clean = pattern.ReplaceAllString(clean, "") // want `sanitizefix: scrubBeforeLaterRange removes a multi-character match in one pass`
	}
	return clean
}

package astx

import (
	"iter"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FormatDirective records byte offsets and zero-based operand positions in a fmt format.
type FormatDirective struct {
	Start, End   int
	Verb         rune
	Flags        string
	Arg, NextArg int
	Indexed      bool
	Star         bool
	Invalid      bool
}

// FormatDirectives walks fmt directives, including literal percent signs and malformed directives.
func FormatDirectives(format string, argCount int) iter.Seq[FormatDirective] {
	return func(yield func(FormatDirective) bool) {
		next := 0
		for offset := 0; offset < len(format); {
			start := strings.IndexByte(format[offset:], '%')
			if start < 0 {
				return
			}
			d := parseFormatDirective(format, offset+start, next, argCount)
			if !yield(d) {
				return
			}
			offset, next = d.End, d.NextArg
		}
	}
}

// FormatVerbLen returns the byte length of an operand verb starting at a percent sign.
func FormatVerbLen(format string, offset int) int {
	d := parseFormatDirective(format, offset, 0, math.MaxInt)
	if d.Invalid || d.Verb == '%' {
		return 0
	}
	return d.End - offset
}

func parseFormatDirective(format string, start, next, argCount int) FormatDirective {
	d := FormatDirective{Start: start}
	i := start + 1
	flagsStart := i
	for i < len(format) && strings.IndexByte("+-# 0", format[i]) >= 0 {
		i++
	}
	d.Flags = format[flagsStart:i]

	readIndex := func() bool {
		if i == len(format) || format[i] != '[' {
			return false
		}
		d.Indexed = true
		end := strings.IndexByte(format[i:], ']')
		if end < 0 {
			d.Invalid = true
			i++
			return false
		}
		raw := format[i+1 : i+end]
		i += end + 1
		index, err := strconv.Atoi(raw)
		if err != nil || strings.HasPrefix(raw, "+") || index < 1 || index > argCount {
			d.Invalid = true
			return false
		}
		next = index - 1
		return true
	}
	readWidth := func() (star, digits bool) {
		if i < len(format) && format[i] == '*' {
			i++
			d.Star = true
			if next < argCount {
				next++
			}
			return true, false
		}
		start := i
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			i++
		}
		return false, i > start
	}

	afterIndex := readIndex()
	if star, digits := readWidth(); star {
		afterIndex = false
	} else if afterIndex && digits {
		d.Invalid = true
	}
	if i+1 < len(format) && format[i] == '.' {
		i++
		d.Invalid = d.Invalid || afterIndex
		afterIndex = readIndex()
		if star, _ := readWidth(); star {
			afterIndex = false
		}
	}
	if !afterIndex {
		readIndex()
	}

	d.Arg = next
	if i == len(format) {
		d.Invalid = true
	} else {
		verb, size := utf8.DecodeRuneInString(format[i:])
		d.Verb = verb
		i += size
		if verb != '%' && !d.Invalid && next < argCount {
			next++
		}
	}
	d.End, d.NextArg = i, next
	return d
}

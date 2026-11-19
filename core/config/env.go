package config

import (
	"os"
	"strconv"
)

// EnvBool reports whether the environment variable key parses as a
// strconv.ParseBool true; unset, empty and unparseable are false.
func EnvBool(key string) bool {
	v := os.Getenv(key)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

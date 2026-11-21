package pgtest

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/lib/pq"
)

// DSNWithSearchPath pins schema on every pool connection, preserving other
// startup settings in either PostgreSQL DSN form.
func DSNWithSearchPath(dsn, schema string) (string, error) {
	if schema == "" {
		return "", errors.New("empty schema name")
	}
	for _, r := range schema {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return "", fmt.Errorf("schema %q is not [a-z0-9_]", schema)
		}
	}
	scoped, err := rewriteSearchPath(dsn, schema)
	if err != nil {
		return "", errors.New("could not parse PostgreSQL DSN")
	}
	if _, err := pq.NewConfig(scoped); err != nil {
		return "", errors.New("invalid PostgreSQL DSN settings")
	}
	return scoped, nil
}

func rewriteSearchPath(dsn, schema string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "", err
		}
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	pairs, err := splitKeywordValueDSN(dsn)
	if err != nil {
		return "", err
	}
	if len(pairs) == 0 {
		return "", errors.New("empty DSN")
	}
	kept := make([]string, 0, len(pairs)+1)
	kept = append(kept, "search_path="+schema)
	for _, pair := range pairs {
		if pair.key != "search_path" {
			kept = append(kept, pair.raw)
		}
	}
	return strings.Join(kept, " "), nil
}

type dsnPair struct{ key, raw string }

func splitKeywordValueDSN(dsn string) ([]dsnPair, error) {
	var out []dsnPair
	i := 0
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	for {
		for i < len(dsn) && isSpace(dsn[i]) {
			i++
		}
		if i >= len(dsn) {
			return out, nil
		}
		start := i
		for i < len(dsn) && dsn[i] != '=' && !isSpace(dsn[i]) {
			i++
		}
		key := dsn[start:i]
		if key == "" {
			return nil, fmt.Errorf("dsn: empty key at offset %d", start)
		}
		for i < len(dsn) && isSpace(dsn[i]) {
			i++
		}
		if i >= len(dsn) || dsn[i] != '=' {
			return nil, fmt.Errorf("dsn: key %q has no value", key)
		}
		i++
		for i < len(dsn) && isSpace(dsn[i]) {
			i++
		}
		if i < len(dsn) && dsn[i] == '\'' {
			i++
			for {
				if i >= len(dsn) {
					return nil, fmt.Errorf("dsn: unterminated quoted value for key %q", key)
				}
				if dsn[i] == '\\' && i+1 < len(dsn) {
					i += 2
					continue
				}
				if dsn[i] == '\'' {
					i++
					break
				}
				i++
			}
		} else {
			for i < len(dsn) && !isSpace(dsn[i]) {
				if dsn[i] == '\\' && i+1 < len(dsn) {
					i += 2
					continue
				}
				i++
			}
		}
		out = append(out, dsnPair{key: key, raw: dsn[start:i]})
	}
}

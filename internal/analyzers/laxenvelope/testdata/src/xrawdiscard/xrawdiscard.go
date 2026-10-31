// want package:"strict\\(xrawdiscard.envelope\\)"

// Package xrawdiscard pins ledger I11 of the 2026-10-27 bug bash: a
// CheckObjectKeys walk whose error is discarded refused nothing, so it
// earns no chokepoint credit, and the lax decodes of the same field
// still fire. The arm-c twin: a discarded deep walk does not cover a
// CheckTopLevelKeys pairing either.
package xrawdiscard

import (
	"encoding/json"
	"strings"
)

type envelope struct {
	Verb   string          `json:"verb"`
	Params json.RawMessage `json:"params"`
}

func UnmarshalStrict(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}

func CheckObjectKeys(data []byte, fold func(string) string) error {
	var v any
	return json.Unmarshal(data, &v)
}

func CheckTopLevelKeys(data []byte, fold func(string) string) error {
	var v any
	return json.Unmarshal(data, &v)
}

func readLoop(line []byte) (envelope, error) {
	var f envelope
	if err := UnmarshalStrict(line, &f); err != nil {
		return f, err
	}
	return f, nil
}

// blankWalk discards the walk's error into _: the keys were walked and
// the verdict thrown away.
func blankWalk(f envelope) error {
	_ = CheckObjectKeys(f.Params, nil)
	var p struct {
		SessionID string `json:"sessionId"`
	}
	return json.Unmarshal(f.Params, &p) // want `lax decode of Params, a json.RawMessage field of envelope`
}

// statementWalk calls the walk as a bare statement.
func statementWalk(f envelope) error {
	CheckObjectKeys(f.Params, nil)
	var v map[string]any
	return json.Unmarshal(f.Params, &v) // want `lax decode of Params, a json.RawMessage field of envelope`
}

type nested struct {
	Arguments map[string]any `json:"arguments"`
}

// topDiscardedDeep pairs CheckTopLevelKeys with a deep walk whose error
// is discarded: the top-level pairing still fires.
func topDiscardedDeep(raw []byte) (nested, error) {
	var v nested
	_ = CheckObjectKeys(raw, strings.ToLower)
	if err := CheckTopLevelKeys(raw, strings.ToLower); err != nil { // want `json.Unmarshal of raw after only handler.CheckTopLevelKeys`
		return v, err
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

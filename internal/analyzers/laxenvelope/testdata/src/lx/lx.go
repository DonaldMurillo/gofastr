// Package lx holds the laxenvelope fixtures: one envelope type decoded
// strictly on one transport and laxly on another, plus the silent
// postures. Identifiers are unrelated to core/mcp's transports on
// purpose: the shape is the split, not the names.
package lx

import (
	"bytes"
	"encoding/json"
	"errors"
)

// frame is the envelope: the strictness contract belongs to the type.
type frame struct {
	Verb   string          `json:"verb"`
	Params json.RawMessage `json:"params"`
}

// UnmarshalStrict is the package's strict decoder. The analyzer keys on
// the name: it is the declared contract, whichever package provides it.
// Its own body decodes through any, so no concrete-type leg fires here.
func UnmarshalStrict(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}

// httpIngest is the strict site: this IS the fix posture, quiet.
func httpIngest(raw []byte) (frame, error) {
	var f frame
	if err := UnmarshalStrict(raw, &f); err != nil {
		return f, err
	}
	return f, nil
}

// streamLoop is the oracle shape: the same frame type, decoded lax on
// the sibling transport.
func streamLoop(line []byte) (frame, error) {
	var f frame
	if err := json.Unmarshal(line, &f); err != nil { // want `lax decode of frame`
		return f, err
	}
	return f, nil
}

// decoderLoop is the json.Decoder spelling of the same split.
func decoderLoop(body []byte) (frame, error) {
	var f frame
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&f); err != nil { // want `lax decode of frame`
		return f, err
	}
	return f, nil
}

// streamLoopPtr is the already-a-pointer destination spelling
// (core/mcp passes req straight through): same fire.
func streamLoopPtr(line []byte, f *frame) error {
	if err := json.Unmarshal(line, f); err != nil { // want `lax decode of frame`
		return err
	}
	return nil
}

// httpIngestPtr is the strict pointer spelling: quiet.
func httpIngestPtr(raw []byte, f *frame) error {
	return UnmarshalStrict(raw, f)
}

// ---- silent postures -------------------------------------------------

// gauge is never strictly decoded: no contract to break, so its lax
// decode is quiet by design.
type gauge struct {
	Level int `json:"level"`
}

func gaugeRead(b []byte) (gauge, error) {
	var g gauge
	err := json.Unmarshal(b, &g)
	return g, err
}

// genericPassthrough decodes through any: no concrete type is visible
// here, and the concrete call sites carry the contract.
func genericPassthrough(raw []byte, dst any) error {
	if err := UnmarshalStrict(raw, dst); err != nil {
		return errors.New("bad frame")
	}
	return json.Unmarshal(raw, dst)
}

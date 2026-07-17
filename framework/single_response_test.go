package framework

import "encoding/json"

type singleMapDecoder struct {
	target *map[string]any
}

func singleMap(target *map[string]any) *singleMapDecoder {
	return &singleMapDecoder{target: target}
}

func (d *singleMapDecoder) UnmarshalJSON(raw []byte) error {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	*d.target = envelope.Data
	return nil
}

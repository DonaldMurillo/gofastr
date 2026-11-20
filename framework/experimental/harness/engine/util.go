package engine

import "encoding/json"

func jsonTryUnmarshal(data []byte, dst any) bool {
	if len(data) == 0 {
		return false
	}
	return json.Unmarshal(data, dst) == nil
}

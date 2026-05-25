package engine

import "encoding/json"

// jsonTryUnmarshal is a forgiving JSON decode used by best-effort
// argv-summary extraction. Failures are silent — the caller falls
// back to a coarser summary.
func jsonTryUnmarshal(data []byte, dst any) bool {
	if len(data) == 0 {
		return false
	}
	return json.Unmarshal(data, dst) == nil
}

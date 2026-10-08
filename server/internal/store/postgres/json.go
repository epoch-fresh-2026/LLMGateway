package postgres

import "encoding/json"

func canonicalJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return value
	}
	// Unmarshal only creates JSON-supported values and rejects non-finite
	// numbers, so re-encoding this value cannot encounter unsupported Go types.
	encoded, _ := json.Marshal(decoded)
	return encoded
}

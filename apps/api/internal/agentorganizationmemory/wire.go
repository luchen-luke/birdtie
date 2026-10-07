package agentorganizationmemory

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

func DecodePut(raw []byte) (PutInput, error) {
	normalized, e := agentmemory.NormalizeStructuredValue(raw)
	if e != nil {
		return PutInput{}, e
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(normalized, &keys) != nil || len(keys) != 7 {
		return PutInput{}, agentmemory.ErrInvalid
	}
	for _, key := range []string{"expectedVersion", "category", "key", "summary", "structuredValue", "visibility", "validUntil"} {
		if value, ok := keys[key]; !ok || string(value) == "null" {
			return PutInput{}, agentmemory.ErrInvalid
		}
	}
	var input PutInput
	if json.Unmarshal(normalized, &input) != nil {
		return PutInput{}, agentmemory.ErrInvalid
	}
	return input, nil
}

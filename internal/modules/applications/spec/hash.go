package appspec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func Hash(spec Spec) (string, error) {
	payload := struct {
		Spec Spec `json:"spec"`
	}{
		Spec: Normalize(spec),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

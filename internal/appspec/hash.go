package appspec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func Hash(spec Spec, variables map[string]string) (string, error) {
	payload := struct {
		Spec      Spec              `json:"spec"`
		Variables map[string]string `json:"variables"`
	}{
		Spec:      Normalize(spec),
		Variables: variables,
	}
	if payload.Variables == nil {
		payload.Variables = map[string]string{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Package contract defines the stable, implementation-independent Agent gRPC
// contract model and compatibility rules shared by the Panel and Agent sides.
package contract

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

var currentContractHash string

type Contract struct {
	ProtoFile *descriptorpb.FileDescriptorProto `json:"protoFile"`
	Methods   []Method                          `json:"methods"`
}

type Method struct {
	ID       string  `json:"id"`
	Service  string  `json:"service"`
	RPC      string  `json:"rpc"`
	Request  *Schema `json:"request,omitempty"`
	Response *Schema `json:"response,omitempty"`
}

type Schema struct {
	Type       string            `json:"type"`
	Optional   bool              `json:"optional,omitempty"`
	Fields     map[string]Schema `json:"fields,omitempty"`
	Items      *Schema           `json:"items,omitempty"`
	Additional *Schema           `json:"additional,omitempty"`
}

func CurrentHash() string {
	return currentContractHash
}

func ValidateGeneratedHash() error {
	return validateGeneratedHash(currentContractHash)
}

func validateGeneratedHash(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("agent gRPC contract hash is empty; run the contract hash generator before building")
	}
	return nil
}

func Hash(contract Contract) (string, error) {
	payload, err := json.Marshal(contract)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum), nil
}

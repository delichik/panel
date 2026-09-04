package certs

import "context"

type Request struct {
	Domain   string
	Domains  []string
	Progress func(context.Context, ACMEProgress)
}

type ACMEProgress struct {
	Stage    string
	Domain   string
	Message  string
	Metadata map[string]any
}

type Bundle struct {
	CertificatePEM []byte
	PrivateKeyPEM  []byte
	CAChainPEM     []byte
}

type Provider interface {
	Issue(ctx context.Context, req Request) (Bundle, error)
}

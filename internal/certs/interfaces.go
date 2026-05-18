package certs

import "context"

type Request struct {
	Domain string
}

type Bundle struct {
	CertificatePEM []byte
	PrivateKeyPEM  []byte
}

type Provider interface {
	Issue(ctx context.Context, req Request) (Bundle, error)
	Renew(ctx context.Context, certID string) (Bundle, error)
}

package certs

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme"

	"panel/internal/config"
	"panel/internal/panelerr"
)

type DNSChallengeProvider interface {
	Present(ctx context.Context, domain, token, value string) error
	CleanUp(ctx context.Context, domain, token, value string) error
}

type ACMEProvider struct {
	client           *acme.Client
	accountEmail     string
	dns              DNSChallengeProvider
	propagationDelay time.Duration
}

func NewACMEProvider(cfg config.Config, dns DNSChallengeProvider, httpClient *http.Client) (Provider, error) {
	if dns == nil {
		return nil, panelerr.Validation("certificate_dns_provider_invalid", "Certificate DNS provider is not configured")
	}
	accountKey, err := loadOrCreateAccountKey(filepath.Join(cfg.DataRoot, "certs", "acme-account.key"))
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := &acme.Client{
		Key:          accountKey,
		DirectoryURL: cfg.Certificates.ACMEDirectoryURL,
		HTTPClient:   httpClient,
	}
	return &ACMEProvider{
		client:           client,
		accountEmail:     cfg.Certificates.Email,
		dns:              dns,
		propagationDelay: time.Duration(cfg.Certificates.DNSPropagationDelaySeconds) * time.Second,
	}, nil
}

func (p *ACMEProvider) Issue(ctx context.Context, req Request) (Bundle, error) {
	domains := req.Domains
	if len(domains) == 0 && req.Domain != "" {
		domains = []string{req.Domain}
	}
	if len(domains) == 0 {
		return Bundle{}, panelerr.Validation("certificate_domain_required", "At least one domain is required")
	}
	account := &acme.Account{}
	if p.accountEmail != "" {
		account.Contact = []string{"mailto:" + p.accountEmail}
	}
	emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_account", Message: "Registering ACME account"})
	if _, err := p.client.Register(ctx, account, acme.AcceptTOS); err != nil {
		return Bundle{}, panelerr.BadGateway("acme_register_failed", "ACME account registration failed: "+err.Error())
	}
	emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_account", Message: "ACME account is ready"})

	identifiers := make([]acme.AuthzID, 0, len(domains))
	for _, domain := range domains {
		identifiers = append(identifiers, acme.AuthzID{Type: "dns", Value: domain})
	}
	emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_order", Message: "Creating ACME order", Metadata: map[string]any{"domains": domains}})
	order, err := p.client.AuthorizeOrder(ctx, identifiers)
	if err != nil {
		return Bundle{}, panelerr.BadGateway("acme_order_failed", "ACME order failed: "+err.Error())
	}
	emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_order", Message: "ACME order created", Metadata: map[string]any{"authorizationCount": len(order.AuthzURLs)}})

	cleanups := []func(){}
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()
	for _, authzURL := range order.AuthzURLs {
		emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_authorization", Message: "Loading ACME authorization"})
		authz, err := p.client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return Bundle{}, panelerr.BadGateway("acme_authorization_failed", "ACME authorization failed: "+err.Error())
		}
		domain := authz.Identifier.Value
		emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_authorization", Domain: domain, Message: "ACME authorization status: " + authz.Status})
		if authz.Status == acme.StatusValid {
			continue
		}
		challenge := dnsChallenge(authz.Challenges)
		if challenge == nil {
			return Bundle{}, panelerr.BadGateway("acme_dns_challenge_missing", "ACME server did not offer a DNS-01 challenge")
		}
		value, err := p.client.DNS01ChallengeRecord(challenge.Token)
		if err != nil {
			return Bundle{}, panelerr.BadGateway("acme_dns_challenge_failed", "ACME DNS-01 challenge setup failed: "+err.Error())
		}
		emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_dns_challenge", Domain: domain, Message: "Creating DNS-01 challenge record"})
		if err := p.dns.Present(ctx, domain, challenge.Token, value); err != nil {
			return Bundle{}, err
		}
		cleanups = append(cleanups, func(domain, token, value string) func() {
			return func() {
				emitACMEProgress(context.Background(), req, ACMEProgress{Stage: "acme_dns_cleanup", Domain: domain, Message: "Cleaning DNS-01 challenge record"})
				_ = p.dns.CleanUp(context.Background(), domain, token, value)
			}
		}(domain, challenge.Token, value))
		if p.propagationDelay > 0 {
			emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_dns_challenge", Domain: domain, Message: "Waiting for DNS propagation", Metadata: map[string]any{"delaySeconds": int(p.propagationDelay.Seconds())}})
			if err := sleepContext(ctx, p.propagationDelay); err != nil {
				return Bundle{}, err
			}
		}
		emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_authorization", Domain: domain, Message: "Submitting DNS-01 challenge"})
		if _, err := p.client.Accept(ctx, challenge); err != nil {
			return Bundle{}, panelerr.BadGateway("acme_challenge_failed", "ACME challenge failed: "+err.Error())
		}
		emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_authorization", Domain: domain, Message: "Waiting for ACME authorization"})
		if _, err := p.client.WaitAuthorization(ctx, authz.URI); err != nil {
			return Bundle{}, panelerr.BadGateway("acme_authorization_failed", "ACME authorization failed: "+err.Error())
		}
		emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_authorization", Domain: domain, Message: "ACME authorization is valid"})
	}
	emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_finalize", Message: "Generating certificate request"})
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Bundle{}, err
	}
	csrDER, err := certificateRequest(certKey, domains)
	if err != nil {
		return Bundle{}, err
	}
	emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_finalize", Message: "Finalizing ACME order"})
	certsDER, _, err := p.client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	if err != nil {
		return Bundle{}, panelerr.BadGateway("acme_finalize_failed", "ACME certificate finalization failed: "+err.Error())
	}
	if len(certsDER) == 0 {
		return Bundle{}, panelerr.BadGateway("acme_finalize_failed", "ACME server returned no certificates")
	}
	certPEM, chainPEM := encodeCertificateChain(certsDER)
	keyPEM, err := encodeECPrivateKey(certKey)
	if err != nil {
		return Bundle{}, err
	}
	emitACMEProgress(ctx, req, ACMEProgress{Stage: "acme_finalize", Message: "ACME certificate bundle received", Metadata: map[string]any{"certificateCount": len(certsDER)}})
	return Bundle{CertificatePEM: certPEM, CAChainPEM: chainPEM, PrivateKeyPEM: keyPEM}, nil
}

func emitACMEProgress(ctx context.Context, req Request, event ACMEProgress) {
	if req.Progress != nil {
		req.Progress(ctx, event)
	}
}

func dnsChallenge(challenges []*acme.Challenge) *acme.Challenge {
	for _, challenge := range challenges {
		if challenge.Type == "dns-01" {
			return challenge
		}
	}
	return nil
}

func loadOrCreateAccountKey(path string) (crypto.Signer, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(raw)
		if block != nil {
			return x509.ParseECPrivateKey(block.Bytes)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func certificateRequest(key crypto.Signer, domains []string) ([]byte, error) {
	tpl := &x509.CertificateRequest{DNSNames: domains}
	return x509.CreateCertificateRequest(rand.Reader, tpl, key)
}

func encodeCertificateChain(certsDER [][]byte) ([]byte, []byte) {
	var leaf []byte
	var chain []byte
	for i, certDER := range certsDER {
		block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		if i == 0 {
			leaf = block
			continue
		}
		chain = append(chain, block...)
	}
	return leaf, chain
}

func encodeECPrivateKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

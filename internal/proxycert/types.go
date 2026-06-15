package proxycert

type Certificate struct {
	ID             string
	Domains        []string
	CertificatePEM string
	PrivateKeyPEM  string
}

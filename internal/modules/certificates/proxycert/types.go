package proxycert

type Certificate struct {
	ID             string
	Name           string
	Source         string
	Domains        []string
	CertificatePEM string
	PrivateKeyPEM  string
}

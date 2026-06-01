package nomad

type Config struct {
	Address    string
	Token      string
	Namespace  string
	Region     string
	Datacenter string
	TLS        *TLSConfig
}

type TLSConfig struct {
	CAFile             string
	CertFile           string
	KeyFile            string
	SkipVerifyHostname bool
}

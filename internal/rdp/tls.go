package rdp

import (
	"crypto/tls"

	"github.com/jetkvm/kvm/internal/websecure"
	"github.com/rs/zerolog"
)

const (
	DefaultTLSStorePath     = "/userdata/jetkvm/rdp_tls"
	selfSignedDefaultDomain = "jetkvm-rdp.local"
	selfSignedOrganization  = "JetKVM"
	selfSignedOU            = "JetKVM RDP"
	selfSignedCACommonName  = "JetKVM RDP Self-Signed CA"
)

// TLSProvider provisions TLS certificates for the embedded RDP listener.
type TLSProvider struct {
	store  *websecure.CertStore
	signer *websecure.SelfSigner
}

func NewTLSProvider(storePath string, logger *zerolog.Logger) *TLSProvider {
	if storePath == "" {
		storePath = DefaultTLSStorePath
	}
	store := websecure.NewCertStore(storePath, logger)
	store.LoadCertificates()
	signer := websecure.NewSelfSigner(
		store,
		logger,
		selfSignedDefaultDomain,
		selfSignedOrganization,
		selfSignedOU,
		selfSignedCACommonName,
	)
	return &TLSProvider{store: store, signer: signer}
}

func (p *TLSProvider) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		MaxVersion:     tls.VersionTLS13,
		GetCertificate: p.signer.GetCertificate,
	}
}

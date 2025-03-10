package authority

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"

	"github.com/pkg/errors"
	"github.com/smallstep/certificates/authority/provisioner"
	"github.com/smallstep/certificates/db"
	"github.com/smallstep/certificates/kms"
	"github.com/smallstep/certificates/sshutil"
	"golang.org/x/crypto/ssh"
)

// Option sets options to the Authority.
type Option func(*Authority) error

// WithDatabase sets an already initialized authority database to a new
// authority. This option is intended to be use on graceful reloads.
func WithDatabase(db db.AuthDB) Option {
	return func(a *Authority) error {
		a.db = db
		return nil
	}
}

// WithGetIdentityFunc sets a custom function to retrieve the identity from
// an external resource.
func WithGetIdentityFunc(fn func(ctx context.Context, p provisioner.Interface, email string) (*provisioner.Identity, error)) Option {
	return func(a *Authority) error {
		a.getIdentityFunc = fn
		return nil
	}
}

// WithSSHBastionFunc sets a custom function to get the bastion for a
// given user-host pair.
func WithSSHBastionFunc(fn func(ctx context.Context, user, host string) (*Bastion, error)) Option {
	return func(a *Authority) error {
		a.sshBastionFunc = fn
		return nil
	}
}

// WithSSHGetHosts sets a custom function to get the bastion for a
// given user-host pair.
func WithSSHGetHosts(fn func(ctx context.Context, cert *x509.Certificate) ([]sshutil.Host, error)) Option {
	return func(a *Authority) error {
		a.sshGetHostsFunc = fn
		return nil
	}
}

// WithSSHCheckHost sets a custom function to check whether a given host is
// step ssh enabled. The token is used to validate the request, while the roots
// are used to validate the token.
func WithSSHCheckHost(fn func(ctx context.Context, principal string, tok string, roots []*x509.Certificate) (bool, error)) Option {
	return func(a *Authority) error {
		a.sshCheckHostFunc = fn
		return nil
	}
}

// WithKeyManager defines the key manager used to get and create keys, and sign
// certificates.
func WithKeyManager(k kms.KeyManager) Option {
	return func(a *Authority) error {
		a.keyManager = k
		return nil
	}
}

// WithX509Signer defines the signer used to sign X509 certificates.
func WithX509Signer(crt *x509.Certificate, s crypto.Signer) Option {
	return func(a *Authority) error {
		a.x509Issuer = crt
		a.x509Signer = s
		return nil
	}
}

// WithSSHUserSigner defines the signer used to sign SSH user certificates.
func WithSSHUserSigner(s crypto.Signer) Option {
	return func(a *Authority) error {
		signer, err := ssh.NewSignerFromSigner(s)
		if err != nil {
			return errors.Wrap(err, "error creating ssh user signer")
		}
		a.sshCAUserCertSignKey = signer
		// Append public key to list of user certs
		pub := signer.PublicKey()
		a.sshCAUserCerts = append(a.sshCAUserCerts, pub)
		a.sshCAUserFederatedCerts = append(a.sshCAUserFederatedCerts, pub)
		return nil
	}
}

// WithSSHHostSigner defines the signer used to sign SSH host certificates.
func WithSSHHostSigner(s crypto.Signer) Option {
	return func(a *Authority) error {
		signer, err := ssh.NewSignerFromSigner(s)
		if err != nil {
			return errors.Wrap(err, "error creating ssh host signer")
		}
		a.sshCAHostCertSignKey = signer
		// Append public key to list of host certs
		pub := signer.PublicKey()
		a.sshCAHostCerts = append(a.sshCAHostCerts, pub)
		a.sshCAHostFederatedCerts = append(a.sshCAHostFederatedCerts, pub)
		return nil
	}
}

// WithX509RootCerts is an option that allows to define the list of root
// certificates to use. This option will replace any root certificate defined
// before.
func WithX509RootCerts(rootCerts ...*x509.Certificate) Option {
	return func(a *Authority) error {
		a.rootX509Certs = rootCerts
		return nil
	}
}

// WithX509FederatedCerts is an option that allows to define the list of
// federated certificates. This option will replace any federated certificate
// defined before.
func WithX509FederatedCerts(certs ...*x509.Certificate) Option {
	return func(a *Authority) error {
		a.federatedX509Certs = certs
		return nil
	}
}

// WithX509RootBundle is an option that allows to define the list of root
// certificates. This option will replace any root certificate defined before.
func WithX509RootBundle(pemCerts []byte) Option {
	return func(a *Authority) error {
		certs, err := readCertificateBundle(pemCerts)
		if err != nil {
			return err
		}
		a.rootX509Certs = certs
		return nil
	}
}

// WithX509FederatedBundle is an option that allows to define the list of
// federated certificates. This option will replace any federated certificate
// defined before.
func WithX509FederatedBundle(pemCerts []byte) Option {
	return func(a *Authority) error {
		certs, err := readCertificateBundle(pemCerts)
		if err != nil {
			return err
		}
		a.federatedX509Certs = certs
		return nil
	}
}

func readCertificateBundle(pemCerts []byte) ([]*x509.Certificate, error) {
	var block *pem.Block
	var certs []*x509.Certificate
	for len(pemCerts) > 0 {
		block, pemCerts = pem.Decode(pemCerts)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}

		certs = append(certs, cert)
	}
	return certs, nil
}

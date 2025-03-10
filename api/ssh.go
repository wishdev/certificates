package api

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/smallstep/certificates/authority"
	"github.com/smallstep/certificates/authority/provisioner"
	"github.com/smallstep/certificates/errs"
	"github.com/smallstep/certificates/sshutil"
	"github.com/smallstep/certificates/templates"
	"golang.org/x/crypto/ssh"
)

// SSHAuthority is the interface implemented by a SSH CA authority.
type SSHAuthority interface {
	SignSSH(ctx context.Context, key ssh.PublicKey, opts provisioner.SSHOptions, signOpts ...provisioner.SignOption) (*ssh.Certificate, error)
	RenewSSH(ctx context.Context, cert *ssh.Certificate) (*ssh.Certificate, error)
	RekeySSH(ctx context.Context, cert *ssh.Certificate, key ssh.PublicKey, signOpts ...provisioner.SignOption) (*ssh.Certificate, error)
	SignSSHAddUser(ctx context.Context, key ssh.PublicKey, cert *ssh.Certificate) (*ssh.Certificate, error)
	GetSSHRoots(ctx context.Context) (*authority.SSHKeys, error)
	GetSSHFederation(ctx context.Context) (*authority.SSHKeys, error)
	GetSSHConfig(ctx context.Context, typ string, data map[string]string) ([]templates.Output, error)
	CheckSSHHost(ctx context.Context, principal string, token string) (bool, error)
	GetSSHHosts(ctx context.Context, cert *x509.Certificate) ([]sshutil.Host, error)
	GetSSHBastion(ctx context.Context, user string, hostname string) (*authority.Bastion, error)
}

// SSHSignRequest is the request body of an SSH certificate request.
type SSHSignRequest struct {
	PublicKey        []byte             `json:"publicKey"` // base64 encoded
	OTT              string             `json:"ott"`
	CertType         string             `json:"certType,omitempty"`
	Principals       []string           `json:"principals,omitempty"`
	ValidAfter       TimeDuration       `json:"validAfter,omitempty"`
	ValidBefore      TimeDuration       `json:"validBefore,omitempty"`
	AddUserPublicKey []byte             `json:"addUserPublicKey,omitempty"`
	KeyID            string             `json:"keyID"`
	IdentityCSR      CertificateRequest `json:"identityCSR,omitempty"`
}

// Validate validates the SSHSignRequest.
func (s *SSHSignRequest) Validate() error {
	switch {
	case s.CertType != "" && s.CertType != provisioner.SSHUserCert && s.CertType != provisioner.SSHHostCert:
		return errors.Errorf("unknown certType %s", s.CertType)
	case len(s.PublicKey) == 0:
		return errors.New("missing or empty publicKey")
	case len(s.OTT) == 0:
		return errors.New("missing or empty ott")
	default:
		// Validate identity signature if provided
		if s.IdentityCSR.CertificateRequest != nil {
			if err := s.IdentityCSR.CertificateRequest.CheckSignature(); err != nil {
				return errors.Wrap(err, "invalid identityCSR")
			}
		}
		return nil
	}
}

// SSHSignResponse is the response object that returns the SSH certificate.
type SSHSignResponse struct {
	Certificate         SSHCertificate  `json:"crt"`
	AddUserCertificate  *SSHCertificate `json:"addUserCrt,omitempty"`
	IdentityCertificate []Certificate   `json:"identityCrt,omitempty"`
}

// SSHRootsResponse represents the response object that returns the SSH user and
// host keys.
type SSHRootsResponse struct {
	UserKeys []SSHPublicKey `json:"userKey,omitempty"`
	HostKeys []SSHPublicKey `json:"hostKey,omitempty"`
}

// SSHCertificate represents the response SSH certificate.
type SSHCertificate struct {
	*ssh.Certificate `json:"omitempty"`
}

// SSHGetHostsResponse is the response object that returns the list of valid
// hosts for SSH.
type SSHGetHostsResponse struct {
	Hosts []sshutil.Host `json:"hosts"`
}

// MarshalJSON implements the json.Marshaler interface. Returns a quoted,
// base64 encoded, openssh wire format version of the certificate.
func (c SSHCertificate) MarshalJSON() ([]byte, error) {
	if c.Certificate == nil {
		return []byte("null"), nil
	}
	s := base64.StdEncoding.EncodeToString(c.Certificate.Marshal())
	return []byte(`"` + s + `"`), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface. The certificate is
// expected to be a quoted, base64 encoded, openssh wire formatted block of bytes.
func (c *SSHCertificate) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Wrap(err, "error decoding certificate")
	}
	if s == "" {
		c.Certificate = nil
		return nil
	}
	certData, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return errors.Wrap(err, "error decoding ssh certificate")
	}
	pub, err := ssh.ParsePublicKey(certData)
	if err != nil {
		return errors.Wrap(err, "error parsing ssh certificate")
	}
	cert, ok := pub.(*ssh.Certificate)
	if !ok {
		return errors.Errorf("error decoding ssh certificate: %T is not an *ssh.Certificate", pub)
	}
	c.Certificate = cert
	return nil
}

// SSHPublicKey represents a public key in a response object.
type SSHPublicKey struct {
	ssh.PublicKey
}

// MarshalJSON implements the json.Marshaler interface. Returns a quoted,
// base64 encoded, openssh wire format version of the public key.
func (p *SSHPublicKey) MarshalJSON() ([]byte, error) {
	if p == nil || p.PublicKey == nil {
		return []byte("null"), nil
	}
	s := base64.StdEncoding.EncodeToString(p.PublicKey.Marshal())
	return []byte(`"` + s + `"`), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface. The public key is
// expected to be a quoted, base64 encoded, openssh wire formatted block of
// bytes.
func (p *SSHPublicKey) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Wrap(err, "error decoding ssh public key")
	}
	if s == "" {
		p.PublicKey = nil
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return errors.Wrap(err, "error decoding ssh public key")
	}
	pub, err := ssh.ParsePublicKey(data)
	if err != nil {
		return errors.Wrap(err, "error parsing ssh public key")
	}
	p.PublicKey = pub
	return nil
}

// Template represents the output of a template.
type Template = templates.Output

// SSHConfigRequest is the request body used to get the SSH configuration
// templates.
type SSHConfigRequest struct {
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

// Validate checks the values of the SSHConfigurationRequest.
func (r *SSHConfigRequest) Validate() error {
	switch r.Type {
	case "":
		r.Type = provisioner.SSHUserCert
		return nil
	case provisioner.SSHUserCert, provisioner.SSHHostCert:
		return nil
	default:
		return errors.Errorf("unsupported type %s", r.Type)
	}
}

// SSHConfigResponse is the response that returns the rendered templates.
type SSHConfigResponse struct {
	UserTemplates []Template `json:"userTemplates,omitempty"`
	HostTemplates []Template `json:"hostTemplates,omitempty"`
}

// SSHCheckPrincipalRequest is the request body used to check if a principal
// certificate has been created. Right now it only supported for hosts
// certificates.
type SSHCheckPrincipalRequest struct {
	Type      string `json:"type"`
	Principal string `json:"principal"`
	Token     string `json:"token,omitempty"`
}

// Validate checks the check principal request.
func (r *SSHCheckPrincipalRequest) Validate() error {
	switch {
	case r.Type != provisioner.SSHHostCert:
		return errors.Errorf("unsupported type %s", r.Type)
	case r.Principal == "":
		return errors.New("missing or empty principal")
	default:
		return nil
	}
}

// SSHCheckPrincipalResponse is the response body used to check if a principal
// exists.
type SSHCheckPrincipalResponse struct {
	Exists bool `json:"exists"`
}

// SSHBastionRequest is the request body used to get the bastion for a given
// host.
type SSHBastionRequest struct {
	User     string `json:"user"`
	Hostname string `json:"hostname"`
}

// Validate checks the values of the SSHBastionRequest.
func (r *SSHBastionRequest) Validate() error {
	if r.Hostname == "" {
		return errors.New("missing or empty hostname")
	}
	return nil
}

// SSHBastionResponse is the response body used to return the bastion for a
// given host.
type SSHBastionResponse struct {
	Hostname string             `json:"hostname"`
	Bastion  *authority.Bastion `json:"bastion,omitempty"`
}

// SSHSign is an HTTP handler that reads an SignSSHRequest with a one-time-token
// (ott) from the body and creates a new SSH certificate with the information in
// the request.
func (h *caHandler) SSHSign(w http.ResponseWriter, r *http.Request) {
	var body SSHSignRequest
	if err := ReadJSON(r.Body, &body); err != nil {
		WriteError(w, errs.Wrap(http.StatusBadRequest, err, "error reading request body"))
		return
	}

	logOtt(w, body.OTT)
	if err := body.Validate(); err != nil {
		WriteError(w, errs.BadRequestErr(err))
		return
	}

	publicKey, err := ssh.ParsePublicKey(body.PublicKey)
	if err != nil {
		WriteError(w, errs.Wrap(http.StatusBadRequest, err, "error parsing publicKey"))
		return
	}

	var addUserPublicKey ssh.PublicKey
	if body.AddUserPublicKey != nil {
		addUserPublicKey, err = ssh.ParsePublicKey(body.AddUserPublicKey)
		if err != nil {
			WriteError(w, errs.Wrap(http.StatusBadRequest, err, "error parsing addUserPublicKey"))
			return
		}
	}

	opts := provisioner.SSHOptions{
		CertType:    body.CertType,
		KeyID:       body.KeyID,
		Principals:  body.Principals,
		ValidBefore: body.ValidBefore,
		ValidAfter:  body.ValidAfter,
	}

	ctx := provisioner.NewContextWithMethod(r.Context(), provisioner.SSHSignMethod)
	signOpts, err := h.Authority.Authorize(ctx, body.OTT)
	if err != nil {
		WriteError(w, errs.UnauthorizedErr(err))
		return
	}

	cert, err := h.Authority.SignSSH(ctx, publicKey, opts, signOpts...)
	if err != nil {
		WriteError(w, errs.ForbiddenErr(err))
		return
	}

	var addUserCertificate *SSHCertificate
	if addUserPublicKey != nil && cert.CertType == ssh.UserCert && len(cert.ValidPrincipals) == 1 {
		addUserCert, err := h.Authority.SignSSHAddUser(ctx, addUserPublicKey, cert)
		if err != nil {
			WriteError(w, errs.ForbiddenErr(err))
			return
		}
		addUserCertificate = &SSHCertificate{addUserCert}
	}

	// Sign identity certificate if available.
	var identityCertificate []Certificate
	if cr := body.IdentityCSR.CertificateRequest; cr != nil {
		ctx := authority.NewContextWithSkipTokenReuse(r.Context())
		ctx = provisioner.NewContextWithMethod(ctx, provisioner.SignMethod)
		signOpts, err := h.Authority.Authorize(ctx, body.OTT)
		if err != nil {
			WriteError(w, errs.UnauthorizedErr(err))
			return
		}

		// Enforce the same duration as ssh certificate.
		signOpts = append(signOpts, &identityModifier{
			NotBefore: time.Unix(int64(cert.ValidAfter), 0),
			NotAfter:  time.Unix(int64(cert.ValidBefore), 0),
		})

		certChain, err := h.Authority.Sign(cr, provisioner.Options{}, signOpts...)
		if err != nil {
			WriteError(w, errs.ForbiddenErr(err))
			return
		}
		identityCertificate = certChainToPEM(certChain)
	}

	JSONStatus(w, &SSHSignResponse{
		Certificate:         SSHCertificate{cert},
		AddUserCertificate:  addUserCertificate,
		IdentityCertificate: identityCertificate,
	}, http.StatusCreated)
}

// SSHRoots is an HTTP handler that returns the SSH public keys for user and host
// certificates.
func (h *caHandler) SSHRoots(w http.ResponseWriter, r *http.Request) {
	keys, err := h.Authority.GetSSHRoots(r.Context())
	if err != nil {
		WriteError(w, errs.InternalServerErr(err))
		return
	}

	if len(keys.HostKeys) == 0 && len(keys.UserKeys) == 0 {
		WriteError(w, errs.NotFound("no keys found"))
		return
	}

	resp := new(SSHRootsResponse)
	for _, k := range keys.HostKeys {
		resp.HostKeys = append(resp.HostKeys, SSHPublicKey{PublicKey: k})
	}
	for _, k := range keys.UserKeys {
		resp.UserKeys = append(resp.UserKeys, SSHPublicKey{PublicKey: k})
	}

	JSON(w, resp)
}

// SSHFederation is an HTTP handler that returns the federated SSH public keys
// for user and host certificates.
func (h *caHandler) SSHFederation(w http.ResponseWriter, r *http.Request) {
	keys, err := h.Authority.GetSSHFederation(r.Context())
	if err != nil {
		WriteError(w, errs.InternalServerErr(err))
		return
	}

	if len(keys.HostKeys) == 0 && len(keys.UserKeys) == 0 {
		WriteError(w, errs.NotFound("no keys found"))
		return
	}

	resp := new(SSHRootsResponse)
	for _, k := range keys.HostKeys {
		resp.HostKeys = append(resp.HostKeys, SSHPublicKey{PublicKey: k})
	}
	for _, k := range keys.UserKeys {
		resp.UserKeys = append(resp.UserKeys, SSHPublicKey{PublicKey: k})
	}

	JSON(w, resp)
}

// SSHConfig is an HTTP handler that returns rendered templates for ssh clients
// and servers.
func (h *caHandler) SSHConfig(w http.ResponseWriter, r *http.Request) {
	var body SSHConfigRequest
	if err := ReadJSON(r.Body, &body); err != nil {
		WriteError(w, errs.Wrap(http.StatusBadRequest, err, "error reading request body"))
		return
	}
	if err := body.Validate(); err != nil {
		WriteError(w, errs.BadRequestErr(err))
		return
	}

	ts, err := h.Authority.GetSSHConfig(r.Context(), body.Type, body.Data)
	if err != nil {
		WriteError(w, errs.InternalServerErr(err))
		return
	}

	var config SSHConfigResponse
	switch body.Type {
	case provisioner.SSHUserCert:
		config.UserTemplates = ts
	case provisioner.SSHHostCert:
		config.HostTemplates = ts
	default:
		WriteError(w, errs.InternalServer("it should hot get here"))
		return
	}

	JSON(w, config)
}

// SSHCheckHost is the HTTP handler that returns if a hosts certificate exists or not.
func (h *caHandler) SSHCheckHost(w http.ResponseWriter, r *http.Request) {
	var body SSHCheckPrincipalRequest
	if err := ReadJSON(r.Body, &body); err != nil {
		WriteError(w, errs.Wrap(http.StatusBadRequest, err, "error reading request body"))
		return
	}
	if err := body.Validate(); err != nil {
		WriteError(w, errs.BadRequestErr(err))
		return
	}

	exists, err := h.Authority.CheckSSHHost(r.Context(), body.Principal, body.Token)
	if err != nil {
		WriteError(w, errs.InternalServerErr(err))
		return
	}
	JSON(w, &SSHCheckPrincipalResponse{
		Exists: exists,
	})
}

// SSHGetHosts is the HTTP handler that returns a list of valid ssh hosts.
func (h *caHandler) SSHGetHosts(w http.ResponseWriter, r *http.Request) {
	var cert *x509.Certificate
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		cert = r.TLS.PeerCertificates[0]
	}

	hosts, err := h.Authority.GetSSHHosts(r.Context(), cert)
	if err != nil {
		WriteError(w, errs.InternalServerErr(err))
		return
	}
	JSON(w, &SSHGetHostsResponse{
		Hosts: hosts,
	})
}

// SSHBastion provides returns the bastion configured if any.
func (h *caHandler) SSHBastion(w http.ResponseWriter, r *http.Request) {
	var body SSHBastionRequest
	if err := ReadJSON(r.Body, &body); err != nil {
		WriteError(w, errs.Wrap(http.StatusBadRequest, err, "error reading request body"))
		return
	}
	if err := body.Validate(); err != nil {
		WriteError(w, errs.BadRequestErr(err))
		return
	}

	bastion, err := h.Authority.GetSSHBastion(r.Context(), body.User, body.Hostname)
	if err != nil {
		WriteError(w, errs.InternalServerErr(err))
		return
	}

	JSON(w, &SSHBastionResponse{
		Hostname: body.Hostname,
		Bastion:  bastion,
	})
}

// identityModifier is a custom modifier used to force a fixed duration.
type identityModifier struct {
	NotBefore time.Time
	NotAfter  time.Time
}

func (m *identityModifier) Enforce(cert *x509.Certificate) error {
	cert.NotBefore = m.NotBefore
	cert.NotAfter = m.NotAfter
	return nil
}

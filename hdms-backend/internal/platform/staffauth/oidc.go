package staffauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"github.com/hito-hospital/hdms/internal/platform/pgtypeconv"
	staffauthstore "github.com/hito-hospital/hdms/internal/platform/staffauth/store"
	"golang.org/x/oauth2"
)

const loginStateTTL = 10 * time.Minute

var (
	ErrStateUnknown     = errors.New("staffauth: unknown, expired or already-used login state")
	ErrTenantNotAllowed = errors.New("staffauth: sign-in is restricted to the hospital tenant")
	ErrDomainNotAllowed = errors.New("staffauth: sign-in is restricted to the configured email domains")
)

// EntraConfig is the hospital's app registration. AllowedEmailDomains may be
// empty, in which case tenant membership alone is the gate (ADR-0012).
type EntraConfig struct {
	TenantID            string
	ClientID            string
	ClientSecret        string
	RedirectURL         string
	AllowedEmailDomains []string
}

// Claims is the subset of the ID token this system cares about. Everything
// else Entra sends is ignored on purpose.
type Claims struct {
	Subject     string
	TenantID    string
	Email       string
	DisplayName string
	EmployeeNo  string
}

type OIDC struct {
	cfg      EntraConfig
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
	pool     *db.Pool
	stateKey []byte
}

// NewOIDC performs discovery once at start-up. A failure here must not stop
// the process: password login has to keep working when Entra is unreachable,
// so the caller logs and leaves Microsoft sign-in disabled.
func NewOIDC(ctx context.Context, cfg EntraConfig, pool *db.Pool, stateKey []byte) (*OIDC, error) {
	issuer := fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", cfg.TenantID)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("staffauth: oidc discovery: %w", err)
	}
	return &OIDC{
		cfg:      cfg,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		pool:     pool,
		stateKey: stateKey,
	}, nil
}

// AuthCodeURL mints a state and a PKCE verifier, stores them server-side, and
// returns the URL to redirect the browser to. The verifier never reaches the
// browser, so a stolen redirect cannot complete the exchange.
func (o *OIDC) AuthCodeURL(ctx context.Context, redirectTo string) (string, error) {
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()

	enc, err := encryptState(verifier, o.stateKey)
	if err != nil {
		return "", err
	}
	q := staffauthstore.New(db.Conn(ctx, o.pool))
	if err := q.CreateLoginState(ctx, staffauthstore.CreateLoginStateParams{
		StateHash:   hashToken(state),
		VerifierEnc: enc,
		RedirectTo:  pgtypeconv.Text(redirectTo),
		ExpiresAt:   pgtypeconv.Timestamptz(time.Now().Add(loginStateTTL)),
	}); err != nil {
		return "", fmt.Errorf("staffauth: store login state: %w", err)
	}

	return o.oauth.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.S256ChallengeOption(verifier),
	), nil
}

// Exchange consumes the state, redeems the code, verifies the ID token and
// returns the claims this system uses. The state row is consumed atomically,
// so a replayed callback fails.
func (o *OIDC) Exchange(ctx context.Context, state, code string) (Claims, error) {
	q := staffauthstore.New(db.Conn(ctx, o.pool))
	row, err := q.ConsumeLoginState(ctx, hashToken(state))
	if err != nil {
		return Claims{}, ErrStateUnknown
	}
	verifier, err := decryptState(row.VerifierEnc, o.stateKey)
	if err != nil {
		return Claims{}, fmt.Errorf("staffauth: decrypt login state: %w", err)
	}

	token, err := o.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Claims{}, fmt.Errorf("staffauth: exchange code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return Claims{}, errors.New("staffauth: no id_token in the token response")
	}
	idToken, err := o.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Claims{}, fmt.Errorf("staffauth: verify id token: %w", err)
	}

	var raw struct {
		OID               string `json:"oid"`
		TID               string `json:"tid"`
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
		EmployeeID        string `json:"employeeId"`
		OnPremSAM         string `json:"onpremisessamaccountname"`
	}
	if err := idToken.Claims(&raw); err != nil {
		return Claims{}, fmt.Errorf("staffauth: read claims: %w", err)
	}

	email := raw.Email
	if email == "" {
		email = raw.PreferredUsername
	}
	claims := Claims{
		Subject:     raw.OID,
		TenantID:    raw.TID,
		Email:       email,
		DisplayName: raw.Name,
		EmployeeNo:  raw.EmployeeID,
	}
	if err := checkClaimsAllowed(o.cfg, claims); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

// checkClaimsAllowed is the gate that replaced the administrator (ADR-0012):
// the right tenant, and — when the deployment lists them — an allowed email
// domain. It is a pure function so the policy is testable without a network.
func checkClaimsAllowed(cfg EntraConfig, claims Claims) error {
	if claims.Subject == "" {
		return errors.New("staffauth: the id token carried no subject")
	}
	if cfg.TenantID != "" && !strings.EqualFold(claims.TenantID, cfg.TenantID) {
		return ErrTenantNotAllowed
	}
	if len(cfg.AllowedEmailDomains) == 0 {
		return nil
	}
	_, domain, found := strings.Cut(claims.Email, "@")
	if !found {
		return ErrDomainNotAllowed
	}
	for _, allowed := range cfg.AllowedEmailDomains {
		if strings.EqualFold(strings.TrimSpace(allowed), domain) {
			return nil
		}
	}
	return ErrDomainNotAllowed
}

func encryptState(verifier string, key []byte) ([]byte, error) {
	return aesGCMSeal([]byte(verifier), key)
}

func decryptState(ciphertext, key []byte) (string, error) {
	plain, err := aesGCMOpen(ciphertext, key)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func aesGCMSeal(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("staffauth: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("staffauth: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("staffauth: read nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func aesGCMOpen(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("staffauth: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("staffauth: new gcm: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("staffauth: ciphertext too short")
	}
	nonce, body := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, nil)
}

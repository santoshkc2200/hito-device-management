package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Device authorization grant (RFC 8628). The site address is a .local name
// that Google refuses as a web redirect URI, so the administrator signs in on
// any device with a short code instead.

const (
	ProviderGoogleDrive = "google_drive"
	ProviderOneDrive    = "onedrive"
)

var (
	ErrInvalidCloudAccount  = errors.New("backup: invalid cloud account")
	ErrAuthorizationPending = errors.New("backup: sign-in is not finished yet")
	ErrSlowDown             = errors.New("backup: the provider asked for slower polling")
	ErrCodeExpired          = errors.New("backup: the sign-in code expired")
	ErrAccessDenied         = errors.New("backup: sign-in was declined")
	// ErrProviderRejected: the provider answered and said no (wrong client ID
	// or secret, wrong tenant). Retrying the same request will not help.
	ErrProviderRejected = errors.New("backup: the provider rejected the request")
	// ErrProviderUnreachable: no usable answer (network, timeout, 5xx).
	ErrProviderUnreachable = errors.New("backup: cannot reach the sign-in provider")
)

// Endpoints are one provider's URLs. DefaultEndpoints has the real ones;
// tests substitute a fake server through DeviceFlow.Endpoints.
type Endpoints struct {
	DeviceURL  string
	TokenURL   string
	ProfileURL string // Google: Drive "about", for the account's email
	DriveURL   string // OneDrive: the signed-in user's drive
	Scope      string
}

func DefaultEndpoints(provider, tenant string) (Endpoints, error) {
	switch provider {
	case ProviderGoogleDrive:
		return Endpoints{
			DeviceURL:  "https://oauth2.googleapis.com/device/code",
			TokenURL:   "https://oauth2.googleapis.com/token",
			ProfileURL: "https://www.googleapis.com/drive/v3/about?fields=user(emailAddress)",
			// drive.file limits HDMS to files it created itself, which is all a
			// backup needs; the device flow does not allow the broader scope.
			Scope: "https://www.googleapis.com/auth/drive.file",
		}, nil
	case ProviderOneDrive:
		if tenant == "" {
			tenant = "common"
		}
		base := "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0/"
		return Endpoints{
			DeviceURL: base + "devicecode",
			TokenURL:  base + "token",
			DriveURL:  "https://graph.microsoft.com/v1.0/me/drive",
			Scope:     "Files.ReadWrite offline_access",
		}, nil
	}
	return Endpoints{}, fmt.Errorf("%w: unknown provider %q", ErrInvalidCloudAccount, provider)
}

type DeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresIn       time.Duration
	Interval        time.Duration
}

type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Expiry       time.Time
}

// RcloneJSON renders the token the way rclone stores it in its config file.
func (t Token) RcloneJSON() (string, error) {
	typ := t.TokenType
	if typ == "" {
		typ = "Bearer"
	}
	b, err := json.Marshal(struct {
		AccessToken  string    `json:"access_token"`
		TokenType    string    `json:"token_type"`
		RefreshToken string    `json:"refresh_token"`
		Expiry       time.Time `json:"expiry"`
	}{t.AccessToken, typ, t.RefreshToken, t.Expiry.UTC()})
	return string(b), err
}

// Profile is what is read from the provider right after sign-in: the account
// email for the console, and for OneDrive the drive rclone must be told about.
type Profile struct {
	Email     string
	DriveID   string
	DriveType string
}

type DeviceFlow struct {
	HTTP      *http.Client
	Endpoints func(provider, tenant string) (Endpoints, error)
	Now       func() time.Time
}

var defaultHTTP = &http.Client{Timeout: 20 * time.Second}

func (f *DeviceFlow) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return defaultHTTP
}

func (f *DeviceFlow) now() time.Time {
	if f.Now != nil {
		return f.Now().UTC()
	}
	return time.Now().UTC()
}

func (f *DeviceFlow) endpoints(provider, tenant string) (Endpoints, error) {
	if f.Endpoints != nil {
		return f.Endpoints(provider, tenant)
	}
	return DefaultEndpoints(provider, tenant)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func (f *DeviceFlow) do(req *http.Request) (int, []byte, error) {
	resp, err := f.client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	return resp.StatusCode, body, nil
}

func (f *DeviceFlow) postForm(ctx context.Context, endpoint string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return f.do(req)
}

// providerFailure turns a non-success answer into a typed error: 5xx is the
// provider's trouble, anything else is a rejection with its stated reason.
func providerFailure(status int, errCode, description string) error {
	if status >= 500 {
		return fmt.Errorf("%w: HTTP %d", ErrProviderUnreachable, status)
	}
	return fmt.Errorf("%w: %s", ErrProviderRejected, firstNonEmpty(description, errCode, http.StatusText(status)))
}

func (f *DeviceFlow) Start(ctx context.Context, provider, clientID, tenant string) (DeviceCode, error) {
	ep, err := f.endpoints(provider, tenant)
	if err != nil {
		return DeviceCode{}, err
	}
	status, body, err := f.postForm(ctx, ep.DeviceURL, url.Values{"client_id": {clientID}, "scope": {ep.Scope}})
	if err != nil {
		return DeviceCode{}, err
	}
	var r struct {
		DeviceCode       string `json:"device_code"`
		UserCode         string `json:"user_code"`
		VerificationURI  string `json:"verification_uri"`
		VerificationURL  string `json:"verification_url"`
		ExpiresIn        int    `json:"expires_in"`
		Interval         int    `json:"interval"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &r)
	if status/100 != 2 || r.DeviceCode == "" || r.UserCode == "" {
		return DeviceCode{}, providerFailure(status, r.Error, r.ErrorDescription)
	}
	interval, expires := r.Interval, r.ExpiresIn
	if interval <= 0 {
		interval = 5
	}
	if expires <= 0 {
		expires = 900
	}
	return DeviceCode{
		DeviceCode: r.DeviceCode, UserCode: r.UserCode,
		VerificationURI: firstNonEmpty(r.VerificationURI, r.VerificationURL),
		ExpiresIn:       time.Duration(expires) * time.Second,
		Interval:        time.Duration(interval) * time.Second,
	}, nil
}

// Exchange asks once whether the person has finished signing in. Until they
// have, it returns ErrAuthorizationPending (or ErrSlowDown); callers wait the
// interval between calls.
func (f *DeviceFlow) Exchange(ctx context.Context, provider, clientID, secret, tenant, deviceCode string) (Token, error) {
	ep, err := f.endpoints(provider, tenant)
	if err != nil {
		return Token{}, err
	}
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	if secret != "" {
		form.Set("client_secret", secret)
	}
	status, body, err := f.postForm(ctx, ep.TokenURL, form)
	if err != nil {
		return Token{}, err
	}
	var r struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &r)
	if status/100 == 2 && r.AccessToken != "" {
		return Token{
			AccessToken: r.AccessToken, RefreshToken: r.RefreshToken, TokenType: r.TokenType,
			Expiry: f.now().Add(time.Duration(r.ExpiresIn) * time.Second),
		}, nil
	}
	switch r.Error {
	case "authorization_pending":
		return Token{}, ErrAuthorizationPending
	case "slow_down":
		return Token{}, ErrSlowDown
	case "expired_token":
		return Token{}, ErrCodeExpired
	case "access_denied", "authorization_declined":
		return Token{}, ErrAccessDenied
	}
	return Token{}, providerFailure(status, r.Error, r.ErrorDescription)
}

func (f *DeviceFlow) getJSON(ctx context.Context, endpoint, accessToken string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	status, body, err := f.do(req)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return providerFailure(status, "", "")
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("%w: unreadable answer", ErrProviderRejected)
	}
	return nil
}

// Profile reads who signed in. For Google that is only the email, which is
// cosmetic; for OneDrive the drive ID and type are required by rclone.
func (f *DeviceFlow) Profile(ctx context.Context, provider, tenant, accessToken string) (Profile, error) {
	ep, err := f.endpoints(provider, tenant)
	if err != nil {
		return Profile{}, err
	}
	if provider == ProviderGoogleDrive {
		var r struct {
			User struct {
				EmailAddress string `json:"emailAddress"`
			} `json:"user"`
		}
		if err := f.getJSON(ctx, ep.ProfileURL, accessToken, &r); err != nil {
			return Profile{}, err
		}
		return Profile{Email: r.User.EmailAddress}, nil
	}
	var r struct {
		ID        string `json:"id"`
		DriveType string `json:"driveType"`
		Owner     struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"owner"`
	}
	if err := f.getJSON(ctx, ep.DriveURL, accessToken, &r); err != nil {
		return Profile{}, err
	}
	if r.ID == "" || r.DriveType == "" {
		return Profile{}, fmt.Errorf("%w: OneDrive returned no drive", ErrProviderRejected)
	}
	return Profile{Email: r.Owner.User.Email, DriveID: r.ID, DriveType: r.DriveType}, nil
}

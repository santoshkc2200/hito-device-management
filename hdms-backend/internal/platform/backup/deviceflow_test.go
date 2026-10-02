package backup_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hito-hospital/hdms/internal/platform/backup"
	"github.com/hito-hospital/hdms/test/cloudfake"
)

func TestDeviceFlowStartExchangeAndProfile(t *testing.T) {
	p := cloudfake.New(t)
	f := p.Flow()
	ctx := context.Background()

	dc, err := f.Start(ctx, backup.ProviderGoogleDrive, "client-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if dc.DeviceCode != cloudfake.DeviceCode || dc.UserCode != "ABCD-EFGH" ||
		dc.VerificationURI != "https://example.test/device" || dc.Interval != 5*time.Second || dc.ExpiresIn != 10*time.Minute {
		t.Fatalf("device code = %+v", dc)
	}

	for _, c := range []struct {
		answer string
		want   error
	}{
		{"pending", backup.ErrAuthorizationPending},
		{"slow_down", backup.ErrSlowDown},
		{"expired", backup.ErrCodeExpired},
		{"denied", backup.ErrAccessDenied},
		{"down", backup.ErrProviderUnreachable},
	} {
		p.Answer(c.answer)
		if _, err := f.Exchange(ctx, backup.ProviderGoogleDrive, "client-1", "secret-1", "", cloudfake.DeviceCode); !errors.Is(err, c.want) {
			t.Fatalf("answer %q: err = %v, want %v", c.answer, err, c.want)
		}
	}

	p.Answer("approve")
	tok, err := f.Exchange(ctx, backup.ProviderGoogleDrive, "client-1", "secret-1", "", cloudfake.DeviceCode)
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" || !tok.Expiry.Equal(p.Now().Add(time.Hour)) {
		t.Fatalf("token = %+v", tok)
	}
	if got := p.LastForm().Get("client_secret"); got != "secret-1" {
		t.Fatalf("client_secret sent = %q", got)
	}
	if got := p.LastForm().Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:device_code" {
		t.Fatalf("grant_type = %q", got)
	}
	js, err := tok.RcloneJSON()
	if err != nil || !strings.Contains(js, `"access_token":"at-1"`) || !strings.Contains(js, `"expiry":"2026-10-02T10:00:00Z"`) {
		t.Fatalf("rclone json = %s (%v)", js, err)
	}

	g, err := f.Profile(ctx, backup.ProviderGoogleDrive, "", "at-1")
	if err != nil || g.Email != "drive-owner@example.test" {
		t.Fatalf("google profile = %+v (%v)", g, err)
	}
	o, err := f.Profile(ctx, backup.ProviderOneDrive, "contoso", "at-1")
	if err != nil || o.DriveID != "b!abc" || o.DriveType != "business" || o.Email != "od-owner@example.test" {
		t.Fatalf("onedrive profile = %+v (%v)", o, err)
	}
}

func TestExchangeOmitsAnEmptyClientSecret(t *testing.T) {
	p := cloudfake.New(t)
	p.Answer("approve")
	if _, err := p.Flow().Exchange(context.Background(), backup.ProviderOneDrive, "c", "", "common", cloudfake.DeviceCode); err != nil {
		t.Fatal(err)
	}
	if _, present := p.LastForm()["client_secret"]; present {
		t.Fatal("OneDrive is a public client; client_secret must not be sent")
	}
}

func TestStartRejectedByTheProviderCarriesItsReason(t *testing.T) {
	p := cloudfake.New(t)
	p.RejectStart("invalid_client", "The OAuth client was not found.")
	_, err := p.Flow().Start(context.Background(), backup.ProviderGoogleDrive, "nope", "")
	if !errors.Is(err, backup.ErrProviderRejected) || !strings.Contains(err.Error(), "The OAuth client was not found.") {
		t.Fatalf("err = %v", err)
	}
}

func TestStartAcceptsMicrosoftsVerificationUriField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"device_code":"d","user_code":"U","verification_uri":"https://microsoft.test/devicelogin","expires_in":900}`))
	}))
	defer srv.Close()
	f := &backup.DeviceFlow{HTTP: srv.Client(), Endpoints: func(string, string) (backup.Endpoints, error) {
		return backup.Endpoints{DeviceURL: srv.URL}, nil
	}}
	dc, err := f.Start(context.Background(), backup.ProviderOneDrive, "c", "common")
	if err != nil || dc.VerificationURI != "https://microsoft.test/devicelogin" || dc.Interval != 5*time.Second {
		t.Fatalf("dc = %+v (%v)", dc, err)
	}
}

func TestDefaultEndpoints(t *testing.T) {
	g, err := backup.DefaultEndpoints(backup.ProviderGoogleDrive, "")
	if err != nil || g.Scope != "https://www.googleapis.com/auth/drive.file" {
		t.Fatalf("google = %+v (%v)", g, err)
	}
	o, err := backup.DefaultEndpoints(backup.ProviderOneDrive, "contoso.onmicrosoft.com")
	if err != nil || o.Scope != "Files.ReadWrite offline_access" ||
		o.TokenURL != "https://login.microsoftonline.com/contoso.onmicrosoft.com/oauth2/v2.0/token" {
		t.Fatalf("onedrive = %+v (%v)", o, err)
	}
	if _, err := backup.DefaultEndpoints("dropbox", ""); !errors.Is(err, backup.ErrInvalidCloudAccount) {
		t.Fatalf("unknown provider err = %v", err)
	}
}

package backup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testClient(t *testing.T) (*LocationClient, *httptest.Server, string) {
	t.Helper()
	l, _, nas := testLocator(t)
	srv := httptest.NewServer(l.InternalHandler())
	t.Cleanup(srv.Close)
	return NewLocationClient(srv.URL), srv, nas
}

func TestLocationClientRoundTrip(t *testing.T) {
	c, _, nas := testClient(t)
	ctx := context.Background()

	top, err := c.Browse(ctx, "")
	if err != nil || len(top.Roots) != 1 || top.Roots[0].Path != nas || !top.Roots[0].Connected {
		t.Fatalf("Browse roots = %+v, %v", top, err)
	}

	f, err := c.CreateFolder(ctx, nas, "hdms-backups")
	if err != nil || f.Path != filepath.Join(nas, "hdms-backups") {
		t.Fatalf("CreateFolder = %+v, %v", f, err)
	}

	listing, err := c.Browse(ctx, nas)
	if err != nil || len(listing.Folders) != 1 || listing.Folders[0].Name != "hdms-backups" {
		t.Fatalf("Browse(nas) = %+v, %v", listing, err)
	}

	check, err := c.Check(ctx, f.Path)
	if err != nil || !check.OK || len(check.Checks) != len(checkOrder) {
		t.Fatalf("Check = %+v, %v", check, err)
	}
}

func TestLocationClientMapsErrors(t *testing.T) {
	c, _, nas := testClient(t)
	ctx := context.Background()
	if err := os.Mkdir(filepath.Join(nas, "taken"), 0o750); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Browse(ctx, "/etc"); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("browse outside err = %v", err)
	}
	if _, err := c.Browse(ctx, filepath.Join(nas, "missing")); !errors.Is(err, ErrLocationNotFound) {
		t.Errorf("browse missing err = %v", err)
	}
	if _, err := c.CreateFolder(ctx, nas, "../x"); !errors.Is(err, ErrInvalidFolderName) {
		t.Errorf("bad name err = %v", err)
	}
	if _, err := c.CreateFolder(ctx, nas, "taken"); !errors.Is(err, ErrFolderExists) {
		t.Errorf("exists err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(nas, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Browse(ctx, filepath.Join(nas, "f.txt")); !errors.Is(err, ErrDriveNotConnected) {
		t.Errorf("browse not-a-directory err = %v", err)
	}
	// An outside path is a check result, not a transport error.
	if check, err := c.Check(ctx, "/etc"); err != nil || check.OK {
		t.Errorf("check outside = %+v, %v", check, err)
	}
}

func TestLocationClientReportsAnUnreachableWorker(t *testing.T) {
	c, srv, nas := testClient(t)
	srv.Close()
	if _, err := c.Browse(context.Background(), nas); !errors.Is(err, ErrWorkerUnavailable) {
		t.Fatalf("closed server err = %v", err)
	}
	var nilClient *LocationClient
	if _, err := nilClient.Check(context.Background(), nas); !errors.Is(err, ErrWorkerUnavailable) {
		t.Fatalf("nil client err = %v", err)
	}
}

func TestInternalHandlerRejectsBadBodies(t *testing.T) {
	_, srv, _ := testClient(t)
	for _, body := range []string{"not json", `{"path":` + `"` + strings.Repeat("a", 8192) + `"}`} {
		resp, err := http.Post(srv.URL+"/internal/locations/check", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %.20q status = %d, want 400", body, resp.StatusCode)
		}
	}
}

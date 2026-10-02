package apiserver

import (
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

func TestMapLocationListingCarriesDriveNameAndHostPath(t *testing.T) {
	got := mapLocationListing(backup.LocationListing{Roots: []backup.LocationRoot{
		{Path: "/drives/usb", Name: "usb", HostPath: "/Volumes/usb", Connected: true},
		{Path: "/drives/empty", Name: "empty"},
	}})
	if r := got.Roots[0]; r.Name != "usb" || r.HostPath == nil || *r.HostPath != "/Volumes/usb" || !r.Connected {
		t.Fatalf("root 0 = %+v", r)
	}
	if r := got.Roots[1]; r.HostPath != nil {
		t.Fatalf("root 1 hostPath = %v, want absent", *r.HostPath)
	}
}

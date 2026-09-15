package staffauth

import "testing"

func TestClaimsAreRefusedFromAnotherTenant(t *testing.T) {
	cfg := EntraConfig{TenantID: "tenant-ours", AllowedEmailDomains: []string{"hospital.example"}}
	claims := Claims{Subject: "oid-1", TenantID: "tenant-theirs", Email: "person@hospital.example"}

	if err := checkClaimsAllowed(cfg, claims); err == nil {
		t.Fatal("a token from another tenant was accepted")
	}
}

func TestClaimsAreRefusedFromAnUnlistedEmailDomain(t *testing.T) {
	cfg := EntraConfig{TenantID: "tenant-ours", AllowedEmailDomains: []string{"hospital.example"}}
	claims := Claims{Subject: "oid-1", TenantID: "tenant-ours", Email: "person@elsewhere.example"}

	if err := checkClaimsAllowed(cfg, claims); err == nil {
		t.Fatal("a token with an unlisted email domain was accepted")
	}
}

func TestClaimsFromTheConfiguredTenantAndDomainAreAccepted(t *testing.T) {
	cfg := EntraConfig{TenantID: "tenant-ours", AllowedEmailDomains: []string{"hospital.example"}}
	claims := Claims{Subject: "oid-1", TenantID: "tenant-ours", Email: "Person@Hospital.Example"}

	if err := checkClaimsAllowed(cfg, claims); err != nil {
		t.Fatalf("checkClaimsAllowed = %v, want nil — the domain match is case-insensitive", err)
	}
}

func TestAnEmptyAllowedDomainListAcceptsAnyDomainInTheTenant(t *testing.T) {
	cfg := EntraConfig{TenantID: "tenant-ours"}
	claims := Claims{Subject: "oid-1", TenantID: "tenant-ours", Email: "person@anywhere.example"}

	if err := checkClaimsAllowed(cfg, claims); err != nil {
		t.Fatalf("checkClaimsAllowed = %v, want nil — tenant membership alone is the gate when no domains are listed", err)
	}
}

package directory

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// LDAPConfig defines connection and mapping settings for an external LDAP / Active Directory service.
type LDAPConfig struct {
	URL          string        // e.g. "ldap://127.0.0.1:389" or "ldaps://127.0.0.1:636"
	BindDN       string        // e.g. "cn=read-only,dc=example,dc=com"
	BindPassword string        // bind password
	BaseDN       string        // search base, e.g. "ou=staff,dc=example,dc=com"
	UserFilter   string        // filter, e.g. "(&(objectClass=person)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))"
	SubjectAttr  string        // stable ID attribute, default "entryUUID" (or "objectGUID", fallback "dn")
	EmpNoAttr    string        // employee number attribute, default "employeeNumber"
	NameAttr     string        // full name attribute, default "displayName" (fallback "cn")
	DeptAttr     string        // department attribute, default "department" (fallback "ou")
	EmailAttr    string        // email attribute, default "mail"
	PhoneAttr    string        // phone attribute, default "telephoneNumber"
	Timeout      time.Duration // dial & search timeout, default 10s
	InsecureTLS  bool          // skip TLS cert verification (test environments only)
}

// LDAPClient implements Client against a real LDAP server.
// It is strictly read-only and never issues any modify/add/delete operations.
type LDAPClient struct {
	cfg LDAPConfig
}

var _ Client = (*LDAPClient)(nil)

// NewLDAPClient constructs a read-only LDAPClient.
func NewLDAPClient(cfg LDAPConfig) *LDAPClient {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.UserFilter == "" {
		cfg.UserFilter = "(objectClass=person)"
	}
	if cfg.SubjectAttr == "" {
		cfg.SubjectAttr = "entryUUID"
	}
	if cfg.EmpNoAttr == "" {
		cfg.EmpNoAttr = "employeeNumber"
	}
	if cfg.NameAttr == "" {
		cfg.NameAttr = "displayName"
	}
	if cfg.DeptAttr == "" {
		cfg.DeptAttr = "department"
	}
	if cfg.EmailAttr == "" {
		cfg.EmailAttr = "mail"
	}
	if cfg.PhoneAttr == "" {
		cfg.PhoneAttr = "telephoneNumber"
	}
	return &LDAPClient{cfg: cfg}
}

// SearchStaff executes a subtree search on BaseDN and maps matching entries to Entry records.
// This operation is strictly read-only.
func (c *LDAPClient) SearchStaff(ctx context.Context) ([]Entry, error) {
	if c.cfg.URL == "" {
		return nil, fmt.Errorf("ldap: URL is required")
	}

	conn, err := ldap.DialURL(
		c.cfg.URL,
		ldap.DialWithDialer(&net.Dialer{
			Timeout: c.cfg.Timeout,
		}),
		ldap.DialWithTLSConfig(&tls.Config{
			InsecureSkipVerify: c.cfg.InsecureTLS, // #nosec G402 -- controlled by operator config
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("ldap: dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if c.cfg.BindDN != "" {
		if err := conn.Bind(c.cfg.BindDN, c.cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("ldap: bind: %w", err)
		}
	}

	attrs := []string{
		c.cfg.SubjectAttr,
		c.cfg.EmpNoAttr,
		c.cfg.NameAttr,
		"cn",
		c.cfg.DeptAttr,
		"ou",
		c.cfg.EmailAttr,
		c.cfg.PhoneAttr,
	}

	searchReq := ldap.NewSearchRequest(
		c.cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		int(c.cfg.Timeout.Seconds()),
		false,
		c.cfg.UserFilter,
		attrs,
		nil,
	)

	sr, err := conn.SearchWithPaging(searchReq, 100)
	if err != nil {
		// If paging is not supported by directory, fall back to non-paged search
		sr, err = conn.Search(searchReq)
		if err != nil {
			return nil, fmt.Errorf("ldap: search: %w", err)
		}
	}

	entries := make([]Entry, 0, len(sr.Entries))
	for _, raw := range sr.Entries {
		subject := raw.GetAttributeValue(c.cfg.SubjectAttr)
		if subject == "" {
			subject = raw.DN
		}
		if subject == "" {
			continue
		}

		name := raw.GetAttributeValue(c.cfg.NameAttr)
		if name == "" {
			name = raw.GetAttributeValue("cn")
		}

		dept := raw.GetAttributeValue(c.cfg.DeptAttr)
		if dept == "" {
			dept = raw.GetAttributeValue("ou")
		}

		empNo := raw.GetAttributeValue(c.cfg.EmpNoAttr)
		email := raw.GetAttributeValue(c.cfg.EmailAttr)
		phone := raw.GetAttributeValue(c.cfg.PhoneAttr)

		entries = append(entries, Entry{
			Subject:    strings.TrimSpace(subject),
			EmployeeNo: strings.TrimSpace(empNo),
			FullName:   strings.TrimSpace(name),
			Department: strings.TrimSpace(dept),
			Email:      strings.TrimSpace(email),
			Phone:      strings.TrimSpace(phone),
		})
	}

	return entries, nil
}

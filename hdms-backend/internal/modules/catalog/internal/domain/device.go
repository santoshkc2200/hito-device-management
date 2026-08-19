// Package domain holds catalog's business rules: validation and the
// device status lifecycle (docs/03-domain-model.md). It is pure — no
// database, no I/O — so these rules are tested without a Postgres
// instance.
package domain

import (
	"errors"
	"strings"
)

// DeviceStatus mirrors the device_status Postgres enum.
type DeviceStatus string

const (
	StatusAvailable   DeviceStatus = "available"
	StatusOnLoan      DeviceStatus = "on_loan"
	StatusMaintenance DeviceStatus = "maintenance"
	StatusRetired     DeviceStatus = "retired"
	StatusLost        DeviceStatus = "lost"
)

// DeviceCondition mirrors the device_condition Postgres enum.
type DeviceCondition string

const (
	ConditionGood    DeviceCondition = "good"
	ConditionFair    DeviceCondition = "fair"
	ConditionDamaged DeviceCondition = "damaged"
)

var (
	ErrAssetTagRequired  = errors.New("catalog: asset tag is required")
	ErrNameRequired      = errors.New("catalog: device name is required")
	ErrInvalidTransition = errors.New("catalog: illegal device status transition")
)

func ValidateAssetTag(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", ErrAssetTagRequired
	}
	return v, nil
}

func ValidateDeviceName(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", ErrNameRequired
	}
	return v, nil
}

// transitions is the device lifecycle from docs/03-domain-model.md.
// on_loan is reached only from available, and conventionally only the
// checkout module ever requests it — enforced by the module-boundary lint
// rule that lets only checkout import catalogapi across modules, not by
// this matrix, which is caller-agnostic.
var transitions = map[DeviceStatus]map[DeviceStatus]bool{
	StatusAvailable:   {StatusOnLoan: true, StatusMaintenance: true, StatusLost: true, StatusRetired: true},
	StatusOnLoan:      {StatusAvailable: true, StatusLost: true},
	StatusMaintenance: {StatusAvailable: true, StatusRetired: true},
	StatusLost:        {StatusAvailable: true, StatusRetired: true},
	StatusRetired:     {}, // terminal
}

// ValidateTransition enforces the device status lifecycle diagram.
func ValidateTransition(from, to DeviceStatus) error {
	if from == to {
		return nil
	}
	if transitions[from][to] {
		return nil
	}
	return ErrInvalidTransition
}

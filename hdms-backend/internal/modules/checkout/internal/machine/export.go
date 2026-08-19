package machine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// MachineDefinition is the serializable shape of session-machine.json.
type MachineDefinition struct {
	Version string                           `json:"version"`
	States  map[SessionState]StateDefinition `json:"states"`
}

// StateDefinition is one state's transitions and optional timeout in session-machine.json.
type StateDefinition struct {
	On    map[InputClass]TransitionRule `json:"on"`
	After map[string]TimeoutTarget      `json:"after,omitempty"`
}

// TransitionRule describes what action to take and which state to enter.
type TransitionRule struct {
	Action        Action       `json:"action"`
	Target        SessionState `json:"target"`
	ClearsPending bool         `json:"clearsPending"`
}

// TimeoutTarget describes where a delayed after-transition leads.
type TimeoutTarget struct {
	Target SessionState `json:"target"`
}

// GenerateMachineDefinition builds the complete MachineDefinition from table.
func GenerateMachineDefinition() MachineDefinition {
	tbl := Table()
	states := make(map[SessionState]StateDefinition, len(allStates))

	for _, s := range allStates {
		on := make(map[InputClass]TransitionRule, len(allClasses))
		for _, c := range allClasses {
			rule := tbl[s][c]
			on[c] = TransitionRule{
				Action:        rule.Action,
				Target:        rule.Target,
				ClearsPending: rule.ClearPending,
			}
		}

		var after map[string]TimeoutTarget
		var timeoutMs int
		switch s {
		case AwaitingUser:
			timeoutMs = AwaitingUserTimeoutMs
		case AwaitingDevice:
			timeoutMs = AwaitingDeviceTimeoutMs
		case Ready:
			timeoutMs = ReadyTimeoutMs
		}

		if timeoutMs > 0 {
			after = map[string]TimeoutTarget{
				strconv.Itoa(timeoutMs): {
					Target: Idle,
				},
			}
		}

		states[s] = StateDefinition{
			On:    on,
			After: after,
		}
	}

	return MachineDefinition{
		Version: Version,
		States:  states,
	}
}

// ExportMachineJSON returns the formatted JSON representation of the machine table.
func ExportMachineJSON() ([]byte, error) {
	def := GenerateMachineDefinition()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(def); err != nil {
		return nil, fmt.Errorf("machine: marshal machine definition: %w", err)
	}
	return buf.Bytes(), nil
}

// Scenario represents one end-to-end replayable sequence in scenarios.json.
type Scenario struct {
	Name         string             `json:"name"`
	InitialState SessionState       `json:"initialState"`
	Inputs       []ScenarioInput    `json:"inputs"`
	Expected     []ScenarioExpected `json:"expected"`
}

// ScenarioInput describes one scan input in a scenario.
type ScenarioInput struct {
	Class                 InputClass `json:"class"`
	Kind                  InputKind  `json:"kind,omitempty"`
	DeviceID              string     `json:"deviceId,omitempty"`
	DeviceStatus          string     `json:"deviceStatus,omitempty"`
	HolderUserID          string     `json:"holderUserId,omitempty"`
	HolderBorrowedAt      *time.Time `json:"holderBorrowedAt,omitempty"`
	UserID                string     `json:"userId,omitempty"`
	UserStatus            string     `json:"userStatus,omitempty"`
	TokenPreview          string     `json:"tokenPreview,omitempty"`
	RevokedAt             *time.Time `json:"revokedAt,omitempty"`
	SameAsPendingWithin3s bool       `json:"sameAsPendingWithin3s,omitempty"`

	// PendingSnapshot optionally configures the initial pending device state
	// for single-step matrix test fixtures.
	PendingDeviceID       string `json:"pendingDeviceId,omitempty"`
	PendingDeviceStatus   string `json:"pendingDeviceStatus,omitempty"`
	PendingDeviceHolderID string `json:"pendingDeviceHolderId,omitempty"`
}

// ScenarioExpected is the expected result after an input step.
type ScenarioExpected struct {
	Action Action       `json:"action"`
	State  SessionState `json:"state"`
}

// GenerateScenarios generates all 7 worked scenarios from docs/04 plus all 52 matrix cells.
func GenerateScenarios() []Scenario {
	var scenarios []Scenario

	// 1. Worked Scenarios (1 through 7) from docs/04-scanning-and-checkout-flows.md
	scenarios = append(scenarios,
		Scenario{
			Name:         "1 — Device first, then user, device in stock -> borrow",
			InitialState: Idle,
			Inputs: []ScenarioInput{
				{Class: ClassDeviceAvailable, Kind: KindDevice, DeviceID: "LAPTOP-07", DeviceStatus: "available"},
				{Class: ClassUserActive, Kind: KindUser, UserID: "CARD-Sharma", UserStatus: "active"},
			},
			Expected: []ScenarioExpected{
				{Action: ActionHoldDevice, State: AwaitingUser},
				{Action: ActionBorrow, State: Ready},
			},
		},
		Scenario{
			Name:         "2 — Device first, then user, user already holds it -> return",
			InitialState: Idle,
			Inputs: []ScenarioInput{
				{Class: ClassDeviceOnLoanOtherUser, Kind: KindDevice, DeviceID: "LAPTOP-07", DeviceStatus: "on_loan", HolderUserID: "CARD-Sharma"},
				{Class: ClassUserActive, Kind: KindUser, UserID: "CARD-Sharma", UserStatus: "active"},
			},
			Expected: []ScenarioExpected{
				{Action: ActionHoldDevice, State: AwaitingUser},
				{Action: ActionReturn, State: Ready},
			},
		},
		Scenario{
			Name:         "3 — User first, then device, device in stock -> borrow",
			InitialState: Idle,
			Inputs: []ScenarioInput{
				{Class: ClassUserActive, Kind: KindUser, UserID: "CARD-Sharma", UserStatus: "active"},
				{Class: ClassDeviceAvailable, Kind: KindDevice, DeviceID: "PROJECTOR-02", DeviceStatus: "available"},
				{Class: ClassDeviceAvailable, Kind: KindDevice, DeviceID: "LAPTOP-07", DeviceStatus: "available"},
			},
			Expected: []ScenarioExpected{
				{Action: ActionSetUser, State: AwaitingDevice},
				{Action: ActionBorrow, State: Ready},
				{Action: ActionBorrow, State: Ready},
			},
		},
		Scenario{
			Name:         "4 — User first, then device they already hold -> return",
			InitialState: Idle,
			Inputs: []ScenarioInput{
				{Class: ClassUserActive, Kind: KindUser, UserID: "CARD-Sharma", UserStatus: "active"},
				{Class: ClassDeviceOnLoanSameUser, Kind: KindDevice, DeviceID: "LAPTOP-07", DeviceStatus: "on_loan", HolderUserID: "CARD-Sharma"},
			},
			Expected: []ScenarioExpected{
				{Action: ActionSetUser, State: AwaitingDevice},
				{Action: ActionReturn, State: Ready},
			},
		},
		Scenario{
			Name:         "5 — Device held by someone else",
			InitialState: Idle,
			Inputs: []ScenarioInput{
				{Class: ClassDeviceOnLoanOtherUser, Kind: KindDevice, DeviceID: "LAPTOP-07", DeviceStatus: "on_loan", HolderUserID: "CARD-Karki"},
				{Class: ClassUserActive, Kind: KindUser, UserID: "CARD-Sharma", UserStatus: "active"},
			},
			Expected: []ScenarioExpected{
				{Action: ActionHoldDevice, State: AwaitingUser},
				{Action: ActionReject, State: Idle},
			},
		},
		Scenario{
			Name:         "6 — Unregistered person, device scanned first -> paper fallback",
			InitialState: Idle,
			Inputs: []ScenarioInput{
				{Class: ClassDeviceAvailable, Kind: KindDevice, DeviceID: "PENDRIVE-11", DeviceStatus: "available"},
				{Class: ClassUnknown, Kind: KindUnknown},
			},
			Expected: []ScenarioExpected{
				{Action: ActionHoldDevice, State: AwaitingUser},
				{Action: ActionReject, State: Idle},
			},
		},
		Scenario{
			Name:         "7 — Lost card that has been reissued",
			InitialState: Idle,
			Inputs: []ScenarioInput{
				{Class: ClassRevoked, Kind: KindRevoked},
			},
			Expected: []ScenarioExpected{
				{Action: ActionReject, State: Idle},
			},
		},
	)

	// 2. Full Matrix Cells Coverage
	tbl := Table()
	for _, s := range allStates {
		for _, c := range allClasses {
			if s == AwaitingUser && (c == ClassUserActive || c == ClassUserSame) {
				// Cover the 3 subcases for dynamic resolution from awaiting_user
				scenarios = append(scenarios,
					Scenario{
						Name:         fmt.Sprintf("matrix: %s + %s (pending available -> borrow)", s, c),
						InitialState: s,
						Inputs: []ScenarioInput{
							{
								Class:                 c,
								Kind:                  KindUser,
								UserID:                "user-1",
								UserStatus:            "active",
								PendingDeviceID:       "dev-1",
								PendingDeviceStatus:   "available",
								PendingDeviceHolderID: "",
							},
						},
						Expected: []ScenarioExpected{
							{Action: ActionBorrow, State: Ready},
						},
					},
					Scenario{
						Name:         fmt.Sprintf("matrix: %s + %s (pending on loan to same user -> return)", s, c),
						InitialState: s,
						Inputs: []ScenarioInput{
							{
								Class:                 c,
								Kind:                  KindUser,
								UserID:                "user-1",
								UserStatus:            "active",
								PendingDeviceID:       "dev-1",
								PendingDeviceStatus:   "on_loan",
								PendingDeviceHolderID: "user-1",
							},
						},
						Expected: []ScenarioExpected{
							{Action: ActionReturn, State: Ready},
						},
					},
					Scenario{
						Name:         fmt.Sprintf("matrix: %s + %s (pending on loan to other user -> reject)", s, c),
						InitialState: s,
						Inputs: []ScenarioInput{
							{
								Class:                 c,
								Kind:                  KindUser,
								UserID:                "user-1",
								UserStatus:            "active",
								PendingDeviceID:       "dev-1",
								PendingDeviceStatus:   "on_loan",
								PendingDeviceHolderID: "user-2",
							},
						},
						Expected: []ScenarioExpected{
							{Action: ActionReject, State: Idle},
						},
					},
				)
				continue
			}

			rule := tbl[s][c]
			input := scenarioInputFor(c)
			if s == AwaitingUser {
				input.PendingDeviceID = "dev-pending-1"
			}

			scenarios = append(scenarios, Scenario{
				Name:         fmt.Sprintf("matrix: %s + %s", s, c),
				InitialState: s,
				Inputs:       []ScenarioInput{input},
				Expected: []ScenarioExpected{
					{Action: rule.Action, State: rule.Target},
				},
			})
		}
	}

	return scenarios
}

func scenarioInputFor(class InputClass) ScenarioInput {
	switch class {
	case ClassDeviceAvailable:
		return ScenarioInput{Class: class, Kind: KindDevice, DeviceID: "dev-avail", DeviceStatus: "available"}
	case ClassDeviceOnLoanSameUser:
		return ScenarioInput{Class: class, Kind: KindDevice, DeviceID: "dev-loan-same", DeviceStatus: "on_loan", HolderUserID: "user-session-1"}
	case ClassDeviceOnLoanOtherUser:
		return ScenarioInput{Class: class, Kind: KindDevice, DeviceID: "dev-loan-other", DeviceStatus: "on_loan", HolderUserID: "user-other-2"}
	case ClassDeviceUnavailable:
		return ScenarioInput{Class: class, Kind: KindDevice, DeviceID: "dev-maint", DeviceStatus: "maintenance"}
	case ClassDeviceDuplicate:
		return ScenarioInput{Class: class, Kind: KindDevice, DeviceID: "dev-dup", DeviceStatus: "available", SameAsPendingWithin3s: true}
	case ClassUserActive:
		return ScenarioInput{Class: class, Kind: KindUser, UserID: "user-switched-2", UserStatus: "active"}
	case ClassUserSame:
		return ScenarioInput{Class: class, Kind: KindUser, UserID: "user-session-1", UserStatus: "active"}
	case ClassUserSuspended:
		return ScenarioInput{Class: class, Kind: KindUser, UserID: "user-susp-1", UserStatus: "suspended"}
	case ClassUserArchived:
		return ScenarioInput{Class: class, Kind: KindUser, UserID: "user-arch-1", UserStatus: "archived"}
	case ClassUnbound:
		return ScenarioInput{Class: class, Kind: KindUnbound}
	case ClassUnknown:
		return ScenarioInput{Class: class, Kind: KindUnknown}
	case ClassRevoked:
		return ScenarioInput{Class: class, Kind: KindRevoked}
	case ClassTimeout:
		return ScenarioInput{Class: class, Kind: KindTimeout}
	default:
		return ScenarioInput{Class: class}
	}
}

// ExportScenariosJSON returns the formatted JSON representation of scenarios.
func ExportScenariosJSON() ([]byte, error) {
	scenarios := GenerateScenarios()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(scenarios); err != nil {
		return nil, fmt.Errorf("machine: encode scenarios: %w", err)
	}
	return buf.Bytes(), nil
}

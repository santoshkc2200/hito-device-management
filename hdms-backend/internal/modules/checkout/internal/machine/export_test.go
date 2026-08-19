package machine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// domainPath finds the file inside packages/domain relative to this test file.
func domainPath(filename string) string {
	_, currentFile, _, _ := runtime.Caller(0)
	// currentFile is <repo>/hdms-backend/internal/modules/checkout/internal/machine/export_test.go
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../../.."))
	return filepath.Join(repoRoot, "hdms-frontend", "packages", "domain", filename)
}

// TestMachineJSONMatchesTable regenerates the JSON in memory and compares
// byte-for-byte with the committed packages/domain/session-machine.json.
func TestMachineJSONMatchesTable(t *testing.T) {
	inMemory, err := ExportMachineJSON()
	if err != nil {
		t.Fatalf("ExportMachineJSON failed: %v", err)
	}

	targetPath := domainPath("session-machine.json")
	committed, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read %s: %v (run task generate / hdms-cli export machine to generate)", targetPath, err)
	}

	if !bytes.Equal(inMemory, committed) {
		t.Errorf("%s is out of date compared to Go table; run 'task generate' to update", targetPath)
	}
}

// TestScenariosJSONMatchesGenerated regenerates scenarios JSON in memory and compares
// byte-for-byte with the committed packages/domain/scenarios.json.
func TestScenariosJSONMatchesGenerated(t *testing.T) {
	inMemory, err := ExportScenariosJSON()
	if err != nil {
		t.Fatalf("ExportScenariosJSON failed: %v", err)
	}

	targetPath := domainPath("scenarios.json")
	committed, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read %s: %v (run task generate / hdms-cli export scenarios to generate)", targetPath, err)
	}

	if !bytes.Equal(inMemory, committed) {
		t.Errorf("%s is out of date compared to Go generator; run 'task generate' to update", targetPath)
	}
}

// TestTableDrivenFromJSON drives the transition table verification from the JSON file itself.
func TestTableDrivenFromJSON(t *testing.T) {
	targetPath := domainPath("session-machine.json")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read %s: %v", targetPath, err)
	}

	var def MachineDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		t.Fatalf("unmarshal %s: %v", targetPath, err)
	}

	if def.Version != Version {
		t.Errorf("JSON version = %q, want %q", def.Version, Version)
	}

	tbl := Table()
	for stateName, stateDef := range def.States {
		for class, ruleDef := range stateDef.On {
			rule, ok := tbl[stateName][class]
			if !ok {
				t.Fatalf("Go table missing rule for state=%q class=%q", stateName, class)
			}
			if rule.Action != ruleDef.Action {
				t.Errorf("state=%q class=%q Action = %q, want %q", stateName, class, rule.Action, ruleDef.Action)
			}
			if rule.Target != ruleDef.Target {
				t.Errorf("state=%q class=%q Target = %q, want %q", stateName, class, rule.Target, ruleDef.Target)
			}
			if rule.ClearPending != ruleDef.ClearsPending {
				t.Errorf("state=%q class=%q ClearPending = %v, want %v", stateName, class, rule.ClearPending, ruleDef.ClearsPending)
			}
		}
	}
}

// TestScenariosReplayThroughGoMachine replays all scenarios through Go's Decide function.
func TestScenariosReplayThroughGoMachine(t *testing.T) {
	targetPath := domainPath("scenarios.json")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read %s: %v", targetPath, err)
	}

	var scenarios []Scenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatalf("unmarshal %s: %v", targetPath, err)
	}

	for _, sc := range scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			currentState := sc.InitialState
			snap := Snapshot{}

			if len(sc.Inputs) != len(sc.Expected) {
				t.Fatalf("mismatched inputs (%d) and expected (%d)", len(sc.Inputs), len(sc.Expected))
			}

			for i, in := range sc.Inputs {
				want := sc.Expected[i]

				if (currentState == AwaitingDevice || currentState == Ready) && snap.UserID == "" {
					snap.UserID = "user-session-1"
				}

				if in.PendingDeviceID != "" {
					snap.PendingDeviceID = in.PendingDeviceID
					snap.PendingDeviceStatus = in.PendingDeviceStatus
					snap.PendingDeviceHolderID = in.PendingDeviceHolderID
				}

				machineInput := Input{
					Kind:                  in.Kind,
					DeviceID:              in.DeviceID,
					DeviceStatus:          in.DeviceStatus,
					HolderUserID:          in.HolderUserID,
					UserID:                in.UserID,
					UserStatus:            in.UserStatus,
					TokenPreview:          in.TokenPreview,
					SameAsPendingWithin3s: in.SameAsPendingWithin3s,
				}
				if in.RevokedAt != nil {
					machineInput.RevokedAt = *in.RevokedAt
				}
				if in.HolderBorrowedAt != nil {
					machineInput.HolderBorrowedAt = *in.HolderBorrowedAt
				}

				decision := Decide(currentState, snap, machineInput)

				if decision.Action != want.Action {
					t.Errorf("step %d: Action = %q, want %q", i, decision.Action, want.Action)
				}
				if decision.NextState != want.State {
					t.Errorf("step %d: NextState = %q, want %q", i, decision.NextState, want.State)
				}

				// Update snapshot for subsequent steps in multi-step scenarios
				if decision.ClearPending {
					snap.PendingDeviceID = ""
					snap.PendingDeviceStatus = ""
					snap.PendingDeviceHolderID = ""
				} else if decision.Action == ActionHoldDevice || decision.Action == ActionReplacePending {
					snap.PendingDeviceID = in.DeviceID
					snap.PendingDeviceStatus = in.DeviceStatus
					snap.PendingDeviceHolderID = in.HolderUserID
				}

				if decision.Action == ActionSetUser || decision.Action == ActionSwitchUser {
					snap.UserID = in.UserID
				}

				currentState = decision.NextState
			}
		})
	}
}

// TestScenariosCoverEveryMatrixCell asserts that the fixture covers every (state × class) cell.
func TestScenariosCoverEveryMatrixCell(t *testing.T) {
	targetPath := domainPath("scenarios.json")
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read %s: %v", targetPath, err)
	}

	var scenarios []Scenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatalf("unmarshal %s: %v", targetPath, err)
	}

	covered := make(map[string]bool)

	for _, sc := range scenarios {
		currState := sc.InitialState
		for i, in := range sc.Inputs {
			key := string(currState) + "/" + string(in.Class)
			covered[key] = true
			currState = sc.Expected[i].State
		}
	}

	for _, s := range AllStates() {
		for _, c := range AllClasses() {
			key := string(s) + "/" + string(c)
			if !covered[key] {
				t.Errorf("matrix cell %s is not covered by any scenario in %s", key, targetPath)
			}
		}
	}
}

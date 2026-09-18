package machine

import (
	"testing"
	"time"
)

// Phase 6.4c. These tests cover the reservation rows added to docs/04's
// matrix: a device scanned while a reservation is in force, by the
// reserver and by someone else, in the window and in the pre-window — and,
// most importantly, the row that must not have changed at all, the
// unreserved walk-up path.
//
// The matrix cells themselves are asserted exhaustively by TestMatrix and
// by the generated scenarios; what is here is the behaviour those cells
// exist to produce.

const (
	reserver   = "user-reserver-1"
	walkUp     = "user-walkup-2"
	reservedID = "res-1"
)

var windowStart = time.Date(2026, 9, 18, 14, 0, 0, 0, time.UTC)

// reservedDeviceScan is a device scan arriving while a reservation is in
// force. The caller — not the machine — decides "in force", which is why
// the same helper serves the in-window and pre-window cases: by the time
// Decide sees it, both look identical, and that is the design.
func reservedDeviceScan(reservedFor string) Input {
	return Input{
		Kind:               KindDevice,
		DeviceID:           "dev-1",
		DeviceStatus:       "available",
		ReservedForUserID:  reservedFor,
		ReservationID:      reservedID,
		ReservationStartAt: windowStart,
	}
}

func TestAReserverScanningTheirOwnDeviceCollectsIt(t *testing.T) {
	for _, state := range []SessionState{AwaitingDevice, Ready} {
		t.Run(state.String(), func(t *testing.T) {
			snap := Snapshot{UserID: reserver}
			d := Decide(state, snap, reservedDeviceScan(reserver))

			if d.Action != ActionBorrow {
				t.Errorf("Action = %q, want %q", d.Action, ActionBorrow)
			}
			if d.NextState != Ready {
				t.Errorf("NextState = %q, want %q", d.NextState, Ready)
			}
			if d.MessageKey != MsgReservationCollected {
				t.Errorf("MessageKey = %q, want %q", d.MessageKey, MsgReservationCollected)
			}
			if d.FulfillsReservationID != reservedID {
				t.Errorf("FulfillsReservationID = %q, want %q — execute cannot mark the reservation collected without it", d.FulfillsReservationID, reservedID)
			}
		})
	}
}

// collectionProducesAnOrdinaryLoan is the plan's named test: the whole
// point of the design is that a collected reservation is not a special
// kind of custody. The Action a collection produces must be exactly the
// Action a walk-up borrow produces, so that everything downstream of the
// machine — loans, custody, returns, overdue — cannot tell them apart.
func TestCollectionProducesAnOrdinaryLoan(t *testing.T) {
	snap := Snapshot{UserID: reserver}

	collected := Decide(Ready, snap, reservedDeviceScan(reserver))
	walkUpBorrow := Decide(Ready, snap, Input{Kind: KindDevice, DeviceID: "dev-2", DeviceStatus: "available"})

	if collected.Action != walkUpBorrow.Action {
		t.Errorf("collection Action = %q, walk-up borrow Action = %q — a collected reservation must produce an ordinary loan", collected.Action, walkUpBorrow.Action)
	}
	if collected.NextState != walkUpBorrow.NextState {
		t.Errorf("collection NextState = %q, walk-up NextState = %q", collected.NextState, walkUpBorrow.NextState)
	}
	// The one permitted difference: the reservation to close, and the
	// confirmation wording that makes the journey feel like it worked.
	if collected.FulfillsReservationID == "" {
		t.Error("collection carries no reservation id")
	}
	if walkUpBorrow.FulfillsReservationID != "" {
		t.Errorf("walk-up borrow carries reservation id %q, want none", walkUpBorrow.FulfillsReservationID)
	}
}

// A walk-up borrower meeting someone else's reservation is refused — and
// the refusal must carry the reason and the time, never a bare
// "unavailable". The message args are the only place the screen can get
// "Reserved for Dr. X from 14:00" from.
func TestAWalkUpBorrowerIsRefusedWithTheReasonAndTheTime(t *testing.T) {
	snap := Snapshot{UserID: walkUp}
	d := Decide(Ready, snap, reservedDeviceScan(reserver))

	if d.Action != ActionReject {
		t.Fatalf("Action = %q, want %q", d.Action, ActionReject)
	}
	if d.MessageKey != MsgDeviceReserved {
		t.Errorf("MessageKey = %q, want %q", d.MessageKey, MsgDeviceReserved)
	}
	if d.MessageArgs["reservedForUserId"] != reserver {
		t.Errorf("reservedForUserId = %v, want %q — the refusal cannot name the reserver without it", d.MessageArgs["reservedForUserId"], reserver)
	}
	if d.MessageArgs["reservationStartAt"] != windowStart {
		t.Errorf("reservationStartAt = %v, want %v — the refusal cannot name the time without it", d.MessageArgs["reservationStartAt"], windowStart)
	}
	if d.NextState != Ready {
		t.Errorf("NextState = %q, want %q — a refusal must not tear down the session", d.NextState, Ready)
	}
}

// theWalkUpPathIsUnchangedForUnreservedDevices — the plan calls this "the
// regression that matters most". Every cell reachable by a device scan
// with no reservation in force must decide exactly what it decided before
// reservations existed.
func TestTheWalkUpPathIsUnchangedForUnreservedDevices(t *testing.T) {
	cases := []struct {
		name       string
		state      SessionState
		snap       Snapshot
		in         Input
		wantAction Action
		wantNext   SessionState
	}{
		{
			name:  "available device from idle is held",
			state: Idle, snap: Snapshot{},
			in:         Input{Kind: KindDevice, DeviceID: "dev-1", DeviceStatus: "available"},
			wantAction: ActionHoldDevice, wantNext: AwaitingUser,
		},
		{
			name:  "available device with a known user borrows",
			state: Ready, snap: Snapshot{UserID: walkUp},
			in:         Input{Kind: KindDevice, DeviceID: "dev-1", DeviceStatus: "available"},
			wantAction: ActionBorrow, wantNext: Ready,
		},
		{
			name:  "own loan returns",
			state: Ready, snap: Snapshot{UserID: walkUp},
			in:         Input{Kind: KindDevice, DeviceID: "dev-1", DeviceStatus: "on_loan", HolderUserID: walkUp},
			wantAction: ActionReturn, wantNext: Ready,
		},
		{
			name:  "someone else's loan is refused",
			state: Ready, snap: Snapshot{UserID: walkUp},
			in:         Input{Kind: KindDevice, DeviceID: "dev-1", DeviceStatus: "on_loan", HolderUserID: reserver},
			wantAction: ActionReject, wantNext: Ready,
		},
		{
			name:  "maintenance is refused",
			state: Ready, snap: Snapshot{UserID: walkUp},
			in:         Input{Kind: KindDevice, DeviceID: "dev-1", DeviceStatus: "maintenance"},
			wantAction: ActionReject, wantNext: Ready,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(tc.state, tc.snap, tc.in)
			if d.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", d.Action, tc.wantAction)
			}
			if d.NextState != tc.wantNext {
				t.Errorf("NextState = %q, want %q", d.NextState, tc.wantNext)
			}
			if d.FulfillsReservationID != "" {
				t.Errorf("FulfillsReservationID = %q on an unreserved scan, want none", d.FulfillsReservationID)
			}
		})
	}
}

// A reservation whose window has not opened — and whose pre-window has not
// been entered — is not in force, so the caller passes no reservation at
// all and the device is borrowable by anyone. This is docs/04's "outside
// it" row.
func TestAReservationOutsideItsWindowDoesNotBlockAWalkUpBorrower(t *testing.T) {
	snap := Snapshot{UserID: walkUp}
	d := Decide(Ready, snap, Input{Kind: KindDevice, DeviceID: "dev-1", DeviceStatus: "available"})

	if d.Action != ActionBorrow {
		t.Errorf("Action = %q, want %q — a future reservation must not make a device unborrowable now", d.Action, ActionBorrow)
	}
}

// Custody outranks a reservation. Someone holding a device whose next
// reservation window has already opened is not at fault and must not be
// punished by the screen: their return is an ordinary return.
func TestAnOverrunningBorrowerCanStillReturnAReservedDevice(t *testing.T) {
	snap := Snapshot{UserID: walkUp}
	in := reservedDeviceScan(reserver)
	in.DeviceStatus = "on_loan"
	in.HolderUserID = walkUp

	d := Decide(Ready, snap, in)

	if d.Action != ActionReturn {
		t.Errorf("Action = %q, want %q — an overrunning borrower must not be refused their own return", d.Action, ActionReturn)
	}
}

// At a kiosk the device is usually scanned first, with no user known yet.
// A reserved device is held exactly as an on-loan one is, and the
// judgement happens when the card is finally scanned — so both outcomes
// have to be right on that second scan.
func TestAPendingReservedDeviceIsAdjudicatedWhenTheUserIsIdentified(t *testing.T) {
	held := Decide(Idle, Snapshot{}, reservedDeviceScan(reserver))
	if held.Action != ActionHoldDevice {
		t.Fatalf("first scan Action = %q, want %q — a reserved device must be held, not refused before we know who is standing there", held.Action, ActionHoldDevice)
	}

	pending := Snapshot{
		PendingDeviceID:                 "dev-1",
		PendingDeviceStatus:             "available",
		PendingDeviceReservedForUserID:  reserver,
		PendingDeviceReservationID:      reservedID,
		PendingDeviceReservationStartAt: windowStart,
	}

	t.Run("the reserver collects", func(t *testing.T) {
		d := Decide(AwaitingUser, pending, Input{Kind: KindUser, UserID: reserver, UserStatus: "active"})
		if d.Action != ActionBorrow {
			t.Errorf("Action = %q, want %q", d.Action, ActionBorrow)
		}
		if d.MessageKey != MsgReservationCollected {
			t.Errorf("MessageKey = %q, want %q", d.MessageKey, MsgReservationCollected)
		}
		if d.FulfillsReservationID != reservedID {
			t.Errorf("FulfillsReservationID = %q, want %q", d.FulfillsReservationID, reservedID)
		}
	})

	t.Run("anyone else is refused", func(t *testing.T) {
		d := Decide(AwaitingUser, pending, Input{Kind: KindUser, UserID: walkUp, UserStatus: "active"})
		if d.Action != ActionReject {
			t.Errorf("Action = %q, want %q", d.Action, ActionReject)
		}
		if d.MessageKey != MsgDeviceReserved {
			t.Errorf("MessageKey = %q, want %q", d.MessageKey, MsgDeviceReserved)
		}
		if d.MessageArgs["pendingDeviceReservedForUserId"] != reserver {
			t.Errorf("pendingDeviceReservedForUserId = %v, want %q", d.MessageArgs["pendingDeviceReservedForUserId"], reserver)
		}
	})
}

// The conflicts-refused metric is keyed off the message key, so the key a
// refusal carries is load-bearing beyond its wording: change it and the
// counter silently stops counting.
func TestAReservationRefusalIsDistinguishableFromOtherRefusals(t *testing.T) {
	reserved := Decide(Ready, Snapshot{UserID: walkUp}, reservedDeviceScan(reserver))
	heldByOther := Decide(Ready, Snapshot{UserID: walkUp}, Input{
		Kind: KindDevice, DeviceID: "dev-2", DeviceStatus: "on_loan", HolderUserID: reserver,
	})

	if reserved.Action != heldByOther.Action {
		t.Fatalf("expected both to be refusals, got %q and %q", reserved.Action, heldByOther.Action)
	}
	if reserved.MessageKey == heldByOther.MessageKey {
		t.Errorf("both refusals carry MessageKey %q — a reservation conflict must be distinguishable from a custody conflict, or the refused-conflict metric counts the wrong thing", reserved.MessageKey)
	}
	if reserved.MessageKey != MsgDeviceReserved {
		t.Errorf("MessageKey = %q, want %q", reserved.MessageKey, MsgDeviceReserved)
	}
}

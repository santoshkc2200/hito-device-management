package machine

// resolveAction answers the one question the live machine and paper
// backfill (2.4b's ResolveHistorical) must never disagree on: given who
// currently holds something (custodyHolderID, "" if nobody) and who is
// standing in front of it now (actorID), is this a borrow, a return, or a
// conflict with whoever else has it?
//
// The live machine calls this from the (AwaitingUser, ClassUserActive)
// and (AwaitingUser, ClassUserSame) rules in table.go, resolving a
// just-identified user against an already-pending device. 2.4b's
// ResolveHistorical calls it directly, over lending.CustodyAt instead of a
// live pending device. One function, two callers, so the kiosk and the
// backfill screen's auto-detection can never disagree (FR-74).
func resolveAction(custodyHolderID, actorID string) Action {
	switch custodyHolderID {
	case "":
		return ActionBorrow
	case actorID:
		return ActionReturn
	default:
		return ActionReject
	}
}

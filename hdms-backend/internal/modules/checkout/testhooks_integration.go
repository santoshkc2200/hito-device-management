//go:build integration

package checkout

// SetFailAfterLoanInsertForTest configures a hook that fires immediately
// after OpenLoan completes inside executeBorrow, before subsequent writes or
// commit (docs/phases/phase-2/2.8-testing.md § 2.8.3). Gated behind the
// `integration` build tag (Taskfile.yml's test:integration task) so this
// failure-injection lever never compiles into the production binary.
func (s *Service) SetFailAfterLoanInsertForTest(fn func() error) {
	s.testHooksMu.Lock()
	defer s.testHooksMu.Unlock()
	s.failAfterLoanInsert = fn
}

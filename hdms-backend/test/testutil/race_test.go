package testutil_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/hito-hospital/hdms/test/testutil"
)

func TestRace_RunsSimultaneouslyAndReturnsErrorsInOrder(t *testing.T) {
	var count int32
	err1 := errors.New("err 1")
	err2 := errors.New("err 2")

	errs := testutil.Race(t,
		func() error {
			atomic.AddInt32(&count, 1)
			return err1
		},
		func() error {
			atomic.AddInt32(&count, 1)
			return nil
		},
		func() error {
			atomic.AddInt32(&count, 1)
			return err2
		},
	)

	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3", len(errs))
	}
	if errs[0] != err1 {
		t.Errorf("errs[0] = %v, want %v", errs[0], err1)
	}
	if errs[1] != nil {
		t.Errorf("errs[1] = %v, want nil", errs[1])
	}
	if errs[2] != err2 {
		t.Errorf("errs[2] = %v, want %v", errs[2], err2)
	}
	if atomic.LoadInt32(&count) != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

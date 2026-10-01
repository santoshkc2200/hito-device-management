package recovery

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiterPerIPAndTotal(t *testing.T) {
	now := testNow
	l := &Limiter{PerIP: 5, Total: 20, Now: func() time.Time { return now }}
	for i := 0; i < 5; i++ {
		if ok, _ := l.Take("10.0.0.1"); !ok {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	ok, wait := l.Take("10.0.0.1")
	if ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("6th attempt in a minute = %v wait %v, want refused within a minute", ok, wait)
	}
	if ok, _ := l.Take("10.0.0.2"); !ok {
		t.Fatal("another IP refused by the per-IP limit")
	}
	now = now.Add(61 * time.Second)
	if ok, _ := l.Take("10.0.0.1"); !ok {
		t.Fatal("per-IP limit did not reset after a minute")
	}
	// 7 taken so far; 13 more from fresh IPs reach the hourly total.
	for i := 0; i < 13; i++ {
		if ok, _ := l.Take(fmt.Sprintf("10.1.0.%d", i)); !ok {
			t.Fatalf("attempt %d under the total refused", i)
		}
	}
	ok, wait = l.Take("10.9.9.9")
	if ok || wait <= 0 || wait > time.Hour {
		t.Fatalf("21st attempt in an hour = %v wait %v, want refused", ok, wait)
	}
	now = now.Add(time.Hour)
	if ok, _ := l.Take("10.9.9.9"); !ok {
		t.Fatal("total limit did not reset after an hour")
	}
}

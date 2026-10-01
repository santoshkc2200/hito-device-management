package recovery

import (
	"sync"
	"time"
)

const (
	perIPWindow = time.Minute
	totalWindow = time.Hour
)

// Limiter bounds recovery-key attempts: PerIP within a minute from one
// client, Total within an hour from everyone. With a 128-bit key it is
// defence in depth, and it keeps the worker's logs readable.
type Limiter struct {
	PerIP int
	Total int
	Now   func() time.Time

	mu   sync.Mutex
	byIP map[string][]time.Time
	all  []time.Time
}

// Take records an attempt if one is allowed, or reports how long to wait.
func (l *Limiter) Take(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	if l.byIP == nil {
		l.byIP = map[string][]time.Time{}
	}
	for k, ts := range l.byIP {
		if kept := keepAfter(ts, now.Add(-perIPWindow)); len(kept) == 0 {
			delete(l.byIP, k)
		} else {
			l.byIP[k] = kept
		}
	}
	l.all = keepAfter(l.all, now.Add(-totalWindow))
	if ts := l.byIP[ip]; len(ts) >= l.PerIP {
		return false, ts[0].Add(perIPWindow).Sub(now)
	}
	if len(l.all) >= l.Total {
		return false, l.all[0].Add(totalWindow).Sub(now)
	}
	l.byIP[ip] = append(l.byIP[ip], now)
	l.all = append(l.all, now)
	return true, 0
}

func keepAfter(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	return ts[i:]
}

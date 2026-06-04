package gateway

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	rpmMu      sync.Mutex
	rpmBuckets = map[string]*rpmBucket{}
	tpmMu      sync.Mutex
	tpmBuckets = map[string]*tpmBucket{}
)

type rpmBucket struct {
	mu         sync.Mutex
	timestamps []time.Time
}

type tpmBucket struct {
	mu      sync.Mutex
	entries []tpmEntry
}

type tpmEntry struct {
	at     time.Time
	tokens int
}

// rpmWait blocks until a request slot is available within the rpm limit.
// key should be "baseURL|modelID" to scope the counter per endpoint+model.
func rpmWait(key string, limit int, logger *zap.Logger) {
	if limit <= 0 {
		return
	}
	rpmMu.Lock()
	b, ok := rpmBuckets[key]
	if !ok {
		b = &rpmBucket{}
		rpmBuckets[key] = b
	}
	rpmMu.Unlock()

	for {
		b.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-60 * time.Second)
		var fresh []time.Time
		for _, t := range b.timestamps {
			if t.After(cutoff) {
				fresh = append(fresh, t)
			}
		}
		b.timestamps = fresh

		if len(b.timestamps) < limit {
			b.timestamps = append(b.timestamps, now)
			b.mu.Unlock()
			return
		}

		waitDur := b.timestamps[0].Add(60*time.Second).Sub(now) + 50*time.Millisecond
		b.mu.Unlock()

		if logger != nil {
			logger.Debug("RPM limit reached, waiting",
				zap.String("key", key),
				zap.Int("limit", limit),
				zap.Duration("wait", waitDur))
		}
		time.Sleep(waitDur)
	}
}

// tpmWait reserves an estimated token budget inside a rolling one-minute window.
// The reservation happens before the provider call so concurrent workers do not
// burst past provider-side TPM limits.
func tpmWait(key string, limit int, tokens int, logger *zap.Logger) {
	if limit <= 0 {
		return
	}
	if tokens <= 0 {
		tokens = 1
	}
	if tokens > limit {
		tokens = limit
	}
	tpmMu.Lock()
	b, ok := tpmBuckets[key]
	if !ok {
		b = &tpmBucket{}
		tpmBuckets[key] = b
	}
	tpmMu.Unlock()

	for {
		b.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-60 * time.Second)
		fresh := b.entries[:0]
		used := 0
		for _, entry := range b.entries {
			if entry.at.After(cutoff) {
				fresh = append(fresh, entry)
				used += entry.tokens
			}
		}
		b.entries = fresh
		if used+tokens <= limit {
			b.entries = append(b.entries, tpmEntry{at: now, tokens: tokens})
			b.mu.Unlock()
			return
		}
		waitDur := b.entries[0].at.Add(60*time.Second).Sub(now) + 50*time.Millisecond
		b.mu.Unlock()

		if logger != nil {
			logger.Debug("TPM limit reached, waiting",
				zap.String("key", key),
				zap.Int("limit", limit),
				zap.Int("tokens", tokens),
				zap.Duration("wait", waitDur))
		}
		time.Sleep(waitDur)
	}
}

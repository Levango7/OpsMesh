// Package circuit implements the circuit breaker pattern for protecting
// external calls from cascading failures.
//
// States:
//   - Closed: normal operation; failures counted.
//   - Open: calls immediately rejected; after timeout transitions to HalfOpen.
//   - HalfOpen: limited probe calls allowed; success resets to Closed, failure re-opens.
package circuit

import (
	"errors"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	// StateClosed allows all calls.
	StateClosed State = iota
	// StateOpen rejects all calls.
	StateOpen
	// StateHalfOpen allows probe calls.
	StateHalfOpen
)

// ErrOpen is returned when the breaker is open.
var ErrOpen = errors.New("circuit breaker: open")

// Breaker is a thread-safe circuit breaker.
type Breaker struct {
	mu          sync.Mutex
	name        string
	maxFailures uint32
	timeout     time.Duration
	failures    uint32
	lastFailure time.Time
	state       State
}

// New creates a new Breaker.
//
//	maxFailures: number of consecutive failures before opening the circuit.
//	timeout: duration to wait in Open state before transitioning to HalfOpen.
func New(name string, maxFailures uint32, timeout time.Duration) *Breaker {
	return &Breaker{
		name:        name,
		maxFailures: maxFailures,
		timeout:     timeout,
		state:       StateClosed,
	}
}

// Execute runs fn through the breaker. If the breaker is open (and timeout
// not yet elapsed) it returns ErrOpen without calling fn. If half-open and
// the probe succeeds the breaker resets to closed.
func (b *Breaker) Execute(fn func() error) error {
	if !b.allow() {
		return ErrOpen
	}
	err := fn()
	b.recordResult(err)
	return err
}

// allow determines if a call should be permitted, handling state transitions.
func (b *Breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(b.lastFailure) >= b.timeout {
			b.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return true
	}
	return false
}

// recordResult updates the breaker after a call.
func (b *Breaker) recordResult(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err == nil {
		b.failures = 0
		b.state = StateClosed
		return
	}

	b.failures++
	b.lastFailure = time.Now()

	if b.state == StateHalfOpen {
		b.state = StateOpen
		return
	}

	if b.failures >= b.maxFailures {
		b.state = StateOpen
	}
}

// State returns the current state of the breaker.
// If the breaker is open and the timeout has elapsed, it transitions to HalfOpen.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateOpen && time.Since(b.lastFailure) >= b.timeout {
		b.state = StateHalfOpen
	}
	return b.state
}

// Name returns the breaker's name.
func (b *Breaker) Name() string {
	return b.name
}

// Reset manually resets the breaker to closed with zero failures.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.state = StateClosed
}

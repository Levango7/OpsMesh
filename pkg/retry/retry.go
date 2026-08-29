// Package retry provides configurable retry logic with exponential backoff
// and jitter for handling transient failures.
package retry

import (
	"errors"
	"math/rand"
	"time"
)

// RetryableError wraps an error to signal it is safe to retry.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// Retryable marks an error as retryable.
func Retryable(err error) error {
	return &RetryableError{Err: err}
}

// IsRetryable reports whether err (or any wrapped error) is a RetryableError.
func IsRetryable(err error) bool {
	var re *RetryableError
	return errors.As(err, &re)
}

// ShouldRetry is a function that determines if an error should be retried.
type ShouldRetry func(error) bool

// DefaultShouldRetry retries if the error is marked as Retryable.
func DefaultShouldRetry(err error) bool {
	return IsRetryable(err)
}

// Config holds retry configuration.
type Config struct {
	MaxRetries    int           // maximum number of retry attempts after the first attempt
	InitialDelay  time.Duration // delay before the first retry
	MaxDelay      time.Duration // cap on the backoff delay
	BackoffFactor float64       // multiplier for each retry (e.g., 2.0 for exponential)
	Jitter        float64       // 0..1 fraction of delay to randomize
	ShouldRetry   ShouldRetry   // nil = retry all errors
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxRetries:    3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        0.2,
		ShouldRetry:   nil, // retry all by default
	}
}

// Do executes fn with retry logic.
//
//	fn: the operation to retry.
//	maxRetries: maximum number of attempts after the first (0 = no retries).
//	initialDelay: base delay before the first retry.
//
// Returns the first successful result or the last error.
func Do(fn func() error, maxRetries int, initialDelay time.Duration) error {
	cfg := DefaultConfig()
	cfg.MaxRetries = maxRetries
	cfg.InitialDelay = initialDelay
	return DoWithConfig(fn, cfg)
}

// DoWithConfig executes fn with the provided retry configuration.
func DoWithConfig(fn func() error, cfg Config) error {
	if cfg.BackoffFactor <= 0 {
		cfg.BackoffFactor = 2.0
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 30 * time.Second
	}

	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			wait := applyJitter(delay, cfg.Jitter)
			time.Sleep(wait)
			delay = time.Duration(float64(delay) * cfg.BackoffFactor)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
		}

		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		// Check if we should retry this error.
		if cfg.ShouldRetry != nil && !cfg.ShouldRetry(err) {
			return err
		}
	}
	return lastErr
}

// applyJitter adds randomness to the delay.
// jitter is a fraction (0..1); the actual delay will be in [delay*(1-jitter), delay*(1+jitter)].
func applyJitter(delay time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return delay
	}
	if jitter > 1 {
		jitter = 1
	}
	delta := rand.Float64() * 2 * jitter // 0..2jitter
	factor := 1.0 - jitter + delta       // (1-jitter)..(1+jitter)
	return time.Duration(float64(delay) * factor)
}

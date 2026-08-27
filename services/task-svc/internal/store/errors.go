package store

import "errors"

// Errors returned by the store.
var (
	ErrTaskNotFound         = errors.New("task not found")
	ErrTaskInvalid          = errors.New("task invalid")
	ErrClaimEpochMismatch   = errors.New("claim epoch mismatch")
	ErrScheduleNotFound     = errors.New("schedule not found")
	ErrBatchNotFound        = errors.New("batch not found")
	ErrResultNotFound       = errors.New("result not found")
)

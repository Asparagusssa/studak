package crud

import (
	"errors"
	"fmt"
)

var (
	ErrBanned          = errors.New("account is banned")
	ErrNotLaunched     = errors.New("dating has not launched")
	ErrNotFound        = errors.New("not found")
	ErrNoProfiles      = errors.New("no more profiles")
	ErrInvalidInput    = errors.New("invalid input")
	ErrAlreadyExists   = errors.New("already exists")
	ErrProfilePaused   = errors.New("profile is paused")
	ErrContactRequired = errors.New("contact is required")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

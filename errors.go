package rudp

import "github.com/pkg/errors"

var (
	errInvalidOperation = errors.New("invalid operation")
	errTimeout          = errors.New("timeout")
)

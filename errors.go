package jsonlipc

import (
	"errors"
	"fmt"
)

type MalformedMessageError struct {
	Message string // The raw line that could not be parsed
}

func (e *MalformedMessageError) Error() string {
	return fmt.Sprintf("malformed IPC message received: \"%s\"", e.Message)
}

func (e *MalformedMessageError) Unwrap() error {
	return errors.New(e.Message)
}

func IsMalformedMessageError(err error) bool {
	var mme *MalformedMessageError
	return errors.As(err, &mme)
}

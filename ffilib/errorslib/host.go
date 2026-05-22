package errorslib

import (
	"errors"
)

type ErrorsHost struct{}

func (h *ErrorsHost) New(text string) error {
	return errors.New(text)
}

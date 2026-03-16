package errorslib

type Errors interface {
	New(text string) error
}

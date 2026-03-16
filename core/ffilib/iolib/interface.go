package iolib

type IO interface {
	ReadAll(r any) ([]byte, error)
}

package store

import "errors"

var (
	ErrBadID         = errors.New("bad id")
	ErrNotFound      = errors.New("not found")
	ErrCanvasDeleted = errors.New("canvas deleted")
)

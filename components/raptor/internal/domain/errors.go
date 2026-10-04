package domain

import "errors"

var ErrInvalid = errors.New("InvalidInput")
var ErrNotFound = errors.New("NotFound")
var ErrConflict = errors.New("Conflict")
var ErrUnavailable = errors.New("Unavailable")
var ErrForbidden = errors.New("Forbidden")

package domain

import "errors"

var ErrNotConfigured = errors.New("not configured")
var ErrScope = errors.New("scope mismatch")
var ErrIdentity = errors.New("identity changed")

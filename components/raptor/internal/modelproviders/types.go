package modelproviders

import (
	"errors"
	"time"
)

type ProviderID string
type ModelID string
type ConnectionStatus string

const (
	StatusPending   ConnectionStatus = "pending"
	StatusConnected ConnectionStatus = "connected"
	StatusCancelled ConnectionStatus = "cancelled"
	StatusExpired   ConnectionStatus = "expired"
	StatusFailed    ConnectionStatus = "failed"
)

var (
	ErrForbidden = errors.New("forbidden")
	ErrBusy      = errors.New("connection already pending")
	ErrClosed    = errors.New("connection session closed")
	ErrNotFound  = errors.New("connection session not found")
)

type ConnectChallenge struct {
	SessionID       string    `json:"sessionId"`
	VerificationURL string    `json:"verificationUrl"`
	UserCode        string    `json:"userCode"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type ConnectStatus struct {
	SessionID string           `json:"sessionId"`
	Status    ConnectionStatus `json:"status"`
	ExpiresAt time.Time        `json:"expiresAt"`
	ErrorCode string           `json:"errorCode,omitempty"`
}

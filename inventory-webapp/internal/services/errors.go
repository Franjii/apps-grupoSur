package services

import (
	"context"
	"database/sql"
	"errors"
)

// querier abstracts QueryRowContext so it works with both *sql.DB and *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// MinPasswordLength is the minimum allowed password length across the application.
const MinPasswordLength = 6

// MaxPasswordLength is the maximum allowed password length (bcrypt truncates at 72 bytes).
const MaxPasswordLength = 72

// Maximum field lengths for input validation.
const (
	MaxNameLength = 255   // for study/supply names
	MaxTextLength = 10000 // for descriptions
)

// Auth errors
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserInactive       = errors.New("user account is inactive")
	ErrUserNotFound       = errors.New("user not found")
)

// Password errors
var (
	ErrPasswordTooShort = errors.New("password too short")
	ErrPasswordTooLong  = errors.New("password too long")
)

// Input length errors
var (
	ErrNameTooLong = errors.New("name exceeds maximum length")
	ErrTextTooLong = errors.New("text exceeds maximum length")
)

// Study errors
var (
	ErrStudyNameExists   = errors.New("study name already exists")
	ErrStudyNotFound     = errors.New("study not found")
	ErrStudyNameRequired = errors.New("study name required")
)

// Supply definition errors
var (
	ErrSupplyDefinitionNameExists   = errors.New("supply definition name already exists for this study")
	ErrSupplyDefinitionNotFound     = errors.New("supply definition not found")
	ErrSupplyDefinitionNameRequired = errors.New("supply definition name required")
	ErrInvalidSupplyType            = errors.New("invalid supply type")
)

// Supply instance errors
var (
	ErrSupplyInstanceNotFound = errors.New("supply instance not found")
)

// Stock errors
var (
	ErrSupplyRequired      = errors.New("supply is required")
	ErrStudyRequired       = errors.New("study is required")
	ErrQuantityRequired    = errors.New("quantity is required")
	ErrQuantityPositive    = errors.New("quantity must be positive")
	ErrInvalidOperation    = errors.New("invalid operation type")
	ErrInsufficientStock   = errors.New("insufficient stock for return")
	ErrSupplyInactive      = errors.New("selected supply is inactive")
	ErrStudyInactive       = errors.New("selected study is inactive")
	ErrSupplyStudyMismatch = errors.New("supply does not belong to selected study")
	ErrNoBulkItems         = errors.New("no bulk items provided")
)

// Delivery errors
var (
	ErrParticipantRequired         = errors.New("participant ID is required")
	ErrSerialNumberRequired        = errors.New("serial number is required")
	ErrSerialNumberExists          = errors.New("serial number already exists")
	ErrItemNotFound                = errors.New("returnable item not found")
	ErrItemNotAvailableForDelivery = errors.New("item not available for delivery")
	ErrItemNotDelivered            = errors.New("item has not been delivered")
	ErrSupplyNotReturnable         = errors.New("supply is not returnable type")
	ErrItemAlreadyFrozen           = errors.New("item is already frozen")
	ErrItemNotFrozen               = errors.New("item is not frozen")
	ErrNoDeliveryBulkItems         = errors.New("no delivery bulk items provided")
	ErrInvalidDeliveryOperation    = errors.New("invalid delivery operation type")
)

// Password reset errors
var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenUsed    = errors.New("token already used")
	ErrTokenInvalid = errors.New("invalid token")
)

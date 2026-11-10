package models

import "time"

const (
	RoleAdmin   = "admin"
	RoleUser    = "user"
	RoleSupport = "support"
)

type User struct {
	ID           string
	Username     string
	PasswordHash string
	FullName     string
	Email        *string // nullable
	Role         string  // "admin", "user", or "support"
	Active       bool
	CreatedAt    time.Time
}

func (u User) IsAdmin() bool {
	return u.Role == RoleAdmin || u.Role == RoleSupport
}

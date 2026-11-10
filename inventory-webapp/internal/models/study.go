package models

import "time"

type Study struct {
	ID        string
	Name      string
	Platform1 *string
	Platform2 *string
	Platform3 *string
	Active    bool
	CreatedAt time.Time
}

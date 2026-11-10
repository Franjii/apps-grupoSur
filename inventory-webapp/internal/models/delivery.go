package models

import "time"

const (
	ItemStatusNotDelivered = "not_delivered"
	ItemStatusDelivered    = "delivered"
	ItemStatusReturned     = "returned"
	ItemStatusFrozen       = "frozen"

	OperationFreeze   = "freeze"
	OperationUnfreeze = "unfreeze"
)

type ReturnableItem struct {
	ID                   string
	SupplyInstanceID     string
	StudyID              string
	SerialNumber         string
	Status               string
	CurrentParticipantID *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type DeliveryLogEntry struct {
	ID            string
	ParticipantID string
	ItemID        string
	SupplyName    string
	SerialNumber  string
	Operation     string
	OperationDate time.Time
	PerformedBy   *string
}

// View models

type ReturnableItemListItem struct {
	ID                   string
	SupplyName           string
	SerialNumber         string
	Status               string
	CurrentParticipantID *string
	StudyName            string
	UpdatedAt            time.Time
}

type DeliveryLogListItem struct {
	ID              string
	ParticipantID   string
	SupplyName      string
	SerialNumber    string
	Operation       string
	PerformedByName *string
	OperationDate   time.Time
}

// ItemStatusLabel returns the Spanish display label for the item status.
func ItemStatusLabel(s string) string {
	switch s {
	case ItemStatusNotDelivered:
		return "No Entregado"
	case ItemStatusDelivered:
		return "Entregado"
	case ItemStatusReturned:
		return "Devuelto"
	case ItemStatusFrozen:
		return "Congelado"
	default:
		return s
	}
}

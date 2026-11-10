package models

import "time"

const (
	OperationDelivery = "delivery"
	OperationReturn   = "return"
	OperationUse      = "use"
)

type StockOperation struct {
	ID               string
	StudyID          string
	SupplyInstanceID string
	Operation        string
	Quantity         int
	Notes            *string
	PerformedBy      string
	OperationDate    time.Time
}

type CurrentStock struct {
	ID               string
	SupplyInstanceID string
	StudyID          string
	Quantity         int
	LastUpdated      time.Time
}

// View models for joined queries

type StockOperationListItem struct {
	ID              string
	StudyName       string
	SupplyName      string
	SupplyType      string
	Operation       string
	Quantity        int
	Notes           *string
	PerformedByName *string
	OperationDate   time.Time
}

type CurrentStockItem struct {
	SupplyInstanceID string
	SupplyName       string
	SupplyType       string
	StudyID          string
	StudyName        string
	Quantity         int
	ReceivedDate     time.Time
	SerialNumbers    string
	ExpirationDate   *string
}

type StockAlert struct {
	SupplyName      string
	StudyName       string
	AlertType       string // "reorder" or "expiration"
	Severity        string // "warning", "urgent", "expired" (expiration alerts only)
	CurrentQuantity int
	Threshold       int
	Detail          string
}

// OperationLabel returns the Spanish display label for the operation type.
func OperationLabel(op string) string {
	switch op {
	case OperationDelivery:
		return "Entrega"
	case OperationReturn:
		return "Retorno"
	case OperationUse:
		return "Uso"
	default:
		return op
	}
}

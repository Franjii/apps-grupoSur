package models

import "time"

const (
	SupplyTypeSingleUse  = "single_use"
	SupplyTypeReturnable = "returnable"
	SupplyTypeDisposable = "disposable"
)

// SupplyDefinition is a template/category of supply, managed in the admin section.
type SupplyDefinition struct {
	ID                     string
	Name                   string
	Type                   string // single_use, returnable, disposable
	StudyID                string
	HasExpirationDate      bool
	HasSerialNumber        bool
	ExpirationAlertEnabled bool
	Active                 bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
	// View field (from JOIN)
	StudyName string
}

// SupplyInstance is a physical inventory unit of a supply definition.
type SupplyInstance struct {
	ID                   string
	SupplyDefinitionID   string
	StudyID              string
	Quantity             int
	ExpirationDate       *string
	LotNumber            *string
	ReorderAlertQuantity *int
	ExpirationAlertDays  *int
	ReceivedDate         time.Time
	Notes                *string
	Active               bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
	// View fields (from JOINs)
	SupplyName string
	StudyName  string
	SupplyType string
}

// SupplyTypeLabel returns the Spanish display label for the supply type.
func SupplyTypeLabel(t string) string {
	switch t {
	case SupplyTypeSingleUse:
		return "Uso Unico"
	case SupplyTypeReturnable:
		return "Retornable"
	case SupplyTypeDisposable:
		return "Descartable"
	default:
		return t
	}
}

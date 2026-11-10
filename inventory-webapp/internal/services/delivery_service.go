package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"inventory-webapp/internal/models"

	"github.com/google/uuid"
)

type DeliveryService struct {
	db *sql.DB
}

func NewDeliveryService(db *sql.DB) *DeliveryService {
	return &DeliveryService{db: db}
}

// GetReturnableItemByID returns a single returnable item by ID with joined names.
func (s *DeliveryService) GetReturnableItemByID(ctx context.Context, id string) (models.ReturnableItemListItem, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT ri.id, sd.name, ri.serial_number, ri.status, ri.current_participant_id,
		        st.name, ri.updated_at
		 FROM returnable_items ri
		 JOIN supply_instances si ON ri.supply_instance_id = si.id
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON ri.study_id = st.id
		 WHERE ri.id = ?`, id)
	item, err := scanReturnableItemListItem(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, ErrItemNotFound
		}
		return item, fmt.Errorf("getting returnable item: %w", err)
	}
	return item, nil
}

// DeliverItem delivers a returnable item to a participant.
func (s *DeliveryService) DeliverItem(ctx context.Context, itemID string, participantID string, userID string) error {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return ErrParticipantRequired
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Check item exists and is available for delivery
	var status, serialNumber, supplyName string
	err = tx.QueryRowContext(ctx,
		`SELECT ri.status, ri.serial_number, sd.name
		 FROM returnable_items ri
		 JOIN supply_instances si ON ri.supply_instance_id = si.id
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 WHERE ri.id = ?`, itemID,
	).Scan(&status, &serialNumber, &supplyName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrItemNotFound
		}
		return fmt.Errorf("checking item: %w", err)
	}

	if status != models.ItemStatusNotDelivered && status != models.ItemStatusReturned {
		return ErrItemNotAvailableForDelivery
	}

	// Update item status
	_, err = tx.ExecContext(ctx,
		`UPDATE returnable_items SET status = ?, current_participant_id = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		models.ItemStatusDelivered, participantID, itemID,
	)
	if err != nil {
		return fmt.Errorf("updating item status: %w", err)
	}

	// Log the delivery
	logID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO delivery_log (id, participant_id, item_id, supply_name, serial_number, operation, performed_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		logID, participantID, itemID, supplyName, serialNumber, models.OperationDelivery, userID,
	)
	if err != nil {
		return fmt.Errorf("logging delivery: %w", err)
	}

	return tx.Commit()
}

// ReturnItem returns a delivered item from a participant.
func (s *DeliveryService) ReturnItem(ctx context.Context, itemID string, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Check item exists and is delivered
	var status, serialNumber, supplyName string
	var participantID sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT ri.status, ri.serial_number, ri.current_participant_id, sd.name
		 FROM returnable_items ri
		 JOIN supply_instances si ON ri.supply_instance_id = si.id
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 WHERE ri.id = ?`, itemID,
	).Scan(&status, &serialNumber, &participantID, &supplyName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrItemNotFound
		}
		return fmt.Errorf("checking item: %w", err)
	}

	if status != models.ItemStatusDelivered {
		return ErrItemNotDelivered
	}

	pid := ""
	if participantID.Valid {
		pid = participantID.String
	}

	// Update item status
	_, err = tx.ExecContext(ctx,
		`UPDATE returnable_items SET status = ?, current_participant_id = NULL, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		models.ItemStatusReturned, itemID,
	)
	if err != nil {
		return fmt.Errorf("updating item status: %w", err)
	}

	// Log the return
	logID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO delivery_log (id, participant_id, item_id, supply_name, serial_number, operation, performed_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		logID, pid, itemID, supplyName, serialNumber, models.OperationReturn, userID,
	)
	if err != nil {
		return fmt.Errorf("logging return: %w", err)
	}

	return tx.Commit()
}

// ListReturnableItems returns paginated returnable items with supply names, optionally filtered by status.
func (s *DeliveryService) ListReturnableItems(ctx context.Context, filter string, page int) ([]models.ReturnableItemListItem, int, error) {
	baseFrom := ` FROM returnable_items ri
		 JOIN supply_instances si ON ri.supply_instance_id = si.id
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON ri.study_id = st.id`

	var where string
	var args []any
	switch filter {
	case models.ItemStatusNotDelivered:
		where = ` WHERE ri.status = ?`
		args = append(args, models.ItemStatusNotDelivered)
	case models.ItemStatusDelivered:
		where = ` WHERE ri.status = ?`
		args = append(args, models.ItemStatusDelivered)
	case models.ItemStatusReturned:
		where = ` WHERE ri.status = ?`
		args = append(args, models.ItemStatusReturned)
	case models.ItemStatusFrozen:
		where = ` WHERE ri.status = ?`
		args = append(args, models.ItemStatusFrozen)
	}

	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+baseFrom+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting returnable items: %w", err)
	}

	offset := (page - 1) * models.PageSize
	query := `SELECT ri.id, sd.name, ri.serial_number, ri.status, ri.current_participant_id,
	                 st.name, ri.updated_at` + baseFrom + where + ` ORDER BY ri.updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, models.PageSize, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing returnable items: %w", err)
	}
	defer rows.Close()

	var items []models.ReturnableItemListItem
	for rows.Next() {
		item, err := scanReturnableItemListItem(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning returnable item: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// ListDeliveryLog returns paginated delivery log entries with joined names.
func (s *DeliveryService) ListDeliveryLog(ctx context.Context, page int) ([]models.DeliveryLogListItem, int, error) {
	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_log`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting delivery log: %w", err)
	}

	offset := (page - 1) * models.PageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT dl.id, dl.participant_id, dl.supply_name, dl.serial_number,
		        dl.operation, u.name, dl.operation_date
		 FROM delivery_log dl
		 LEFT JOIN users u ON dl.performed_by = u.id
		 ORDER BY dl.operation_date DESC
		 LIMIT ? OFFSET ?`,
		models.PageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("listing delivery log: %w", err)
	}
	defer rows.Close()

	var items []models.DeliveryLogListItem
	for rows.Next() {
		item, err := scanDeliveryLogListItem(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning delivery log: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// BulkDeliveryItem represents an item in a bulk freeze/unfreeze operation.
type BulkDeliveryItem struct {
	ItemID string `json:"item_id"`
	Status string `json:"status"`
}

// BulkFreezeUnfreeze freezes or unfreezes multiple returnable items in a single transaction.
func (s *DeliveryService) BulkFreezeUnfreeze(ctx context.Context, operation string, items []BulkDeliveryItem, userID string) error {
	if operation != models.OperationFreeze && operation != models.OperationUnfreeze {
		return ErrInvalidDeliveryOperation
	}
	if len(items) == 0 {
		return ErrNoDeliveryBulkItems
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	for _, item := range items {
		var status, serialNumber, supplyName string
		var participantID sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT ri.status, ri.serial_number, ri.current_participant_id, sd.name
			 FROM returnable_items ri
			 JOIN supply_instances si ON ri.supply_instance_id = si.id
			 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
			 WHERE ri.id = ?`, item.ItemID,
		).Scan(&status, &serialNumber, &participantID, &supplyName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrItemNotFound
			}
			return fmt.Errorf("checking item %s: %w", item.ItemID, err)
		}

		// Validate status matches operation
		if operation == models.OperationFreeze && status == models.ItemStatusFrozen {
			return ErrItemAlreadyFrozen
		}
		if operation == models.OperationUnfreeze && status != models.ItemStatusFrozen {
			return ErrItemNotFrozen
		}

		// Determine new status
		newStatus := models.ItemStatusFrozen
		if operation == models.OperationUnfreeze {
			newStatus = models.ItemStatusNotDelivered
		}

		// Update item status
		_, err = tx.ExecContext(ctx,
			`UPDATE returnable_items SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			newStatus, item.ItemID,
		)
		if err != nil {
			return fmt.Errorf("updating item %s: %w", item.ItemID, err)
		}

		// Log the operation
		pid := ""
		if participantID.Valid {
			pid = participantID.String
		}
		logID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO delivery_log (id, participant_id, item_id, supply_name, serial_number, operation, performed_by)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			logID, pid, item.ItemID, supplyName, serialNumber, operation, userID,
		)
		if err != nil {
			return fmt.Errorf("logging %s for item %s: %w", operation, item.ItemID, err)
		}
	}

	return tx.Commit()
}

// GetAvailableItemsForDelivery returns items that can be delivered (not_delivered or returned).
func (s *DeliveryService) GetAvailableItemsForDelivery(ctx context.Context) ([]models.ReturnableItemListItem, error) {
	// Return all items (unpaginated) for delivery form dropdowns
	items, _, err := s.ListReturnableItems(ctx, "", 1)
	// For dropdown usage, we want all items - use a large page or dedicated query
	// Since this is for form dropdowns, use the operation-based query instead
	return items, err
}

// GetAvailableItemsByOperation returns items with appropriate status for the given operation.
func (s *DeliveryService) GetAvailableItemsByOperation(ctx context.Context, operation string) ([]models.ReturnableItemListItem, error) {
	query := `SELECT ri.id, sd.name, ri.serial_number, ri.status, ri.current_participant_id,
	                 st.name, ri.updated_at
		 FROM returnable_items ri
		 JOIN supply_instances si ON ri.supply_instance_id = si.id
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON ri.study_id = st.id`

	var args []any
	switch operation {
	case models.OperationDelivery:
		query += ` WHERE ri.status IN (?, ?)`
		args = append(args, models.ItemStatusNotDelivered, models.ItemStatusReturned)
	case models.OperationReturn:
		query += ` WHERE ri.status = ?`
		args = append(args, models.ItemStatusDelivered)
	}
	query += ` ORDER BY sd.name, ri.serial_number`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing available items: %w", err)
	}
	defer rows.Close()

	var items []models.ReturnableItemListItem
	for rows.Next() {
		item, err := scanReturnableItemListItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning available item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

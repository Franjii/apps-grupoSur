package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"inventory-webapp/internal/models"

	"github.com/google/uuid"
)

type StockService struct {
	db *sql.DB
}

func NewStockService(db *sql.DB) *StockService {
	return &StockService{db: db}
}

type CreateStockOperationInput struct {
	StudyID            string
	SupplyDefinitionID string
	Operation          string
	Quantity           int
	Notes              string
	SerialNumbers      []string
	ExpirationDate     string
}

// RecordOperation records a stock operation (delivery, return, or use) transactionally.
func (s *StockService) RecordOperation(ctx context.Context, input CreateStockOperationInput, userID string) error {
	if input.SupplyDefinitionID == "" {
		return ErrSupplyRequired
	}
	if input.StudyID == "" {
		return ErrStudyRequired
	}
	if input.Quantity <= 0 {
		return ErrQuantityPositive
	}
	if input.Operation != models.OperationDelivery && input.Operation != models.OperationReturn && input.Operation != models.OperationUse {
		return ErrInvalidOperation
	}

	input.Notes = strings.TrimSpace(input.Notes)
	input.ExpirationDate = strings.TrimSpace(input.ExpirationDate)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Validate study is active
	var studyActive bool
	err = tx.QueryRowContext(ctx, "SELECT active FROM studies WHERE id = ?", input.StudyID).Scan(&studyActive)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStudyRequired
		}
		return fmt.Errorf("checking study: %w", err)
	}
	if !studyActive {
		return ErrStudyInactive
	}

	// Validate supply definition is active and belongs to selected study
	var defActive bool
	var defStudyID, defType string
	err = tx.QueryRowContext(ctx,
		"SELECT active, study_id, type FROM supply_definitions WHERE id = ?",
		input.SupplyDefinitionID,
	).Scan(&defActive, &defStudyID, &defType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSupplyRequired
		}
		return fmt.Errorf("checking supply definition: %w", err)
	}
	if !defActive {
		return ErrSupplyInactive
	}
	if defStudyID != input.StudyID {
		return ErrSupplyStudyMismatch
	}

	// Find existing supply instance for this definition+study, or create one
	var instanceID string
	err = tx.QueryRowContext(ctx,
		"SELECT id FROM supply_instances WHERE supply_definition_id = ? AND study_id = ? AND active = 1 LIMIT 1",
		input.SupplyDefinitionID, input.StudyID,
	).Scan(&instanceID)
	if errors.Is(err, sql.ErrNoRows) {
		// Create a minimal instance
		instanceID = uuid.New().String()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO supply_instances (id, supply_definition_id, study_id, quantity, received_by)
			 VALUES (?, ?, ?, 0, ?)`,
			instanceID, input.SupplyDefinitionID, input.StudyID, userID,
		)
		if err != nil {
			return fmt.Errorf("creating supply instance: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("finding supply instance: %w", err)
	}

	// For returns, check sufficient stock
	if input.Operation == models.OperationReturn {
		var currentQty int
		err = tx.QueryRowContext(ctx,
			"SELECT COALESCE(quantity, 0) FROM current_stock WHERE supply_instance_id = ? AND study_id = ?",
			instanceID, input.StudyID,
		).Scan(&currentQty)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking current stock: %w", err)
		}
		if currentQty < input.Quantity {
			return ErrInsufficientStock
		}
	}

	// Insert stock operation
	opID := uuid.New().String()
	var notesParam any
	if input.Notes != "" {
		notesParam = input.Notes
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO stock_operations (id, study_id, supply_instance_id, operation, quantity, notes, performed_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		opID, input.StudyID, instanceID, input.Operation, input.Quantity, notesParam, userID,
	)
	if err != nil {
		return fmt.Errorf("inserting stock operation: %w", err)
	}

	// Insert serial numbers if provided
	for _, sn := range input.SerialNumbers {
		sn = strings.TrimSpace(sn)
		if sn == "" {
			continue
		}
		snOpID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO stock_operation_serial_numbers (id, stock_operation_id, serial_number)
			 VALUES (?, ?, ?)`,
			snOpID, opID, sn,
		)
		if err != nil {
			return fmt.Errorf("inserting operation serial number: %w", err)
		}

		snInstID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO supply_instance_serial_numbers (id, supply_instance_id, serial_number)
			 VALUES (?, ?, ?)`,
			snInstID, instanceID, sn,
		)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return ErrSerialNumberExists
			}
			return fmt.Errorf("inserting instance serial number: %w", err)
		}

		// Create returnable_items entry for returnable supplies
		if defType == models.SupplyTypeReturnable {
			riID := uuid.New().String()
			_, err = tx.ExecContext(ctx,
				`INSERT INTO returnable_items (id, supply_instance_id, study_id, serial_number, status)
				 VALUES (?, ?, ?, ?, ?)`,
				riID, instanceID, input.StudyID, sn, models.ItemStatusNotDelivered,
			)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE constraint failed") {
					return ErrSerialNumberExists
				}
				return fmt.Errorf("inserting returnable item: %w", err)
			}
		}
	}

	// Update expiration date if provided
	if input.ExpirationDate != "" {
		_, err = tx.ExecContext(ctx,
			`UPDATE supply_instances SET expiration_date = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			input.ExpirationDate, instanceID,
		)
		if err != nil {
			return fmt.Errorf("updating expiration date: %w", err)
		}
	}

	// Upsert current_stock
	delta := input.Quantity
	if input.Operation == models.OperationReturn || input.Operation == models.OperationUse {
		delta = -input.Quantity
	}
	csID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO current_stock (id, supply_instance_id, study_id, quantity, last_updated)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(supply_instance_id, study_id) DO UPDATE SET
		   quantity = quantity + ?,
		   last_updated = CURRENT_TIMESTAMP`,
		csID, instanceID, input.StudyID, delta, delta,
	)
	if err != nil {
		return fmt.Errorf("upserting current stock: %w", err)
	}

	return tx.Commit()
}

// BulkOperationItem represents a single item in a bulk stock operation.
type BulkOperationItem struct {
	SupplyInstanceID string `json:"supply_instance_id"`
	StudyID          string `json:"study_id"`
	Quantity         int    `json:"quantity"`
}

// BulkOperation processes multiple return/use operations in a single transaction.
func (s *StockService) BulkOperation(ctx context.Context, operation string, items []BulkOperationItem, userID string) error {
	if operation != models.OperationReturn && operation != models.OperationUse {
		return ErrInvalidOperation
	}
	if len(items) == 0 {
		return ErrNoBulkItems
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	for _, item := range items {
		if item.Quantity <= 0 {
			return ErrQuantityPositive
		}

		// Verify sufficient stock
		var currentQty int
		err := tx.QueryRowContext(ctx,
			"SELECT COALESCE(quantity, 0) FROM current_stock WHERE supply_instance_id = ? AND study_id = ?",
			item.SupplyInstanceID, item.StudyID,
		).Scan(&currentQty)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking current stock: %w", err)
		}
		if currentQty < item.Quantity {
			return ErrInsufficientStock
		}

		// Insert stock operation
		opID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO stock_operations (id, study_id, supply_instance_id, operation, quantity, performed_by)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			opID, item.StudyID, item.SupplyInstanceID, operation, item.Quantity, userID,
		)
		if err != nil {
			return fmt.Errorf("inserting stock operation: %w", err)
		}

		// Decrease current_stock
		_, err = tx.ExecContext(ctx,
			`UPDATE current_stock SET quantity = quantity - ?, last_updated = CURRENT_TIMESTAMP
			 WHERE supply_instance_id = ? AND study_id = ?`,
			item.Quantity, item.SupplyInstanceID, item.StudyID,
		)
		if err != nil {
			return fmt.Errorf("updating current stock: %w", err)
		}

		// Remove eligible returnable_items for returnable supplies
		var defType string
		err = tx.QueryRowContext(ctx,
			`SELECT sd.type FROM supply_definitions sd
			 JOIN supply_instances si ON si.supply_definition_id = sd.id
			 WHERE si.id = ?`, item.SupplyInstanceID,
		).Scan(&defType)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking supply type: %w", err)
		}
		if defType == models.SupplyTypeReturnable {
			_, err = tx.ExecContext(ctx,
				`DELETE FROM returnable_items WHERE id IN (
					SELECT id FROM returnable_items
					WHERE supply_instance_id = ? AND study_id = ?
					ORDER BY created_at ASC
					LIMIT ?
				)`,
				item.SupplyInstanceID, item.StudyID,
				item.Quantity,
			)
			if err != nil {
				return fmt.Errorf("removing returnable items: %w", err)
			}
		}
	}

	return tx.Commit()
}

// GetCurrentStock returns paginated current stock entries with supply and study names.
func (s *StockService) GetCurrentStock(ctx context.Context, page int) ([]models.CurrentStockItem, int, error) {
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		 FROM current_stock cs
		 JOIN supply_instances si ON cs.supply_instance_id = si.id
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON cs.study_id = st.id
		 WHERE cs.quantity > 0`,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting current stock: %w", err)
	}

	offset := (page - 1) * models.PageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT cs.supply_instance_id, sd.name, sd.type, cs.study_id, st.name,
		        cs.quantity, si.received_date,
		        (SELECT GROUP_CONCAT(sisn.serial_number, ', ')
		         FROM supply_instance_serial_numbers sisn
		         WHERE sisn.supply_instance_id = si.id) AS serial_numbers,
		        si.expiration_date
		 FROM current_stock cs
		 JOIN supply_instances si ON cs.supply_instance_id = si.id
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON cs.study_id = st.id
		 WHERE cs.quantity > 0
		 ORDER BY sd.name, st.name
		 LIMIT ? OFFSET ?`,
		models.PageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("listing current stock: %w", err)
	}
	defer rows.Close()

	var items []models.CurrentStockItem
	for rows.Next() {
		item, err := scanCurrentStockItem(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning current stock: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// ListOperations returns paginated stock operations with joined names.
func (s *StockService) ListOperations(ctx context.Context, page int) ([]models.StockOperationListItem, int, error) {
	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock_operations`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting operations: %w", err)
	}

	offset := (page - 1) * models.PageSize
	rows, err := s.db.QueryContext(ctx,
		`SELECT so.id, st.name, sd.name, sd.type, so.operation, so.quantity,
		        so.notes, u.name, so.operation_date
		 FROM stock_operations so
		 JOIN studies st ON so.study_id = st.id
		 JOIN supply_instances si ON so.supply_instance_id = si.id
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 LEFT JOIN users u ON so.performed_by = u.id
		 ORDER BY so.operation_date DESC
		 LIMIT ? OFFSET ?`,
		models.PageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("listing operations: %w", err)
	}
	defer rows.Close()

	var items []models.StockOperationListItem
	for rows.Next() {
		item, err := scanStockOperationListItem(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning operation: %w", err)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// GetStockAlerts returns reorder and expiration alerts based on supply instances.
func (s *StockService) GetStockAlerts(ctx context.Context) ([]models.StockAlert, error) {
	var alerts []models.StockAlert

	// Reorder alerts: supply instances with reorder_alert_quantity set
	rows, err := s.db.QueryContext(ctx,
		`SELECT sd.name, st.name, COALESCE(cs.quantity, 0), si.reorder_alert_quantity
		 FROM supply_instances si
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON si.study_id = st.id
		 LEFT JOIN current_stock cs ON si.id = cs.supply_instance_id AND si.study_id = cs.study_id
		 WHERE si.active = 1
		   AND si.reorder_alert_quantity IS NOT NULL
		   AND COALESCE(cs.quantity, 0) <= si.reorder_alert_quantity
		 ORDER BY sd.name, st.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("checking reorder alerts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var a models.StockAlert
		var studyName sql.NullString
		if err := rows.Scan(&a.SupplyName, &studyName, &a.CurrentQuantity, &a.Threshold); err != nil {
			return nil, fmt.Errorf("scanning reorder alert: %w", err)
		}
		a.AlertType = "reorder"
		if studyName.Valid {
			a.StudyName = studyName.String
		} else {
			a.StudyName = "Sin asignar"
		}
		a.Detail = fmt.Sprintf("Stock actual: %d (umbral: %d)", a.CurrentQuantity, a.Threshold)
		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Expiration alerts from supply instances (within 60 days or already expired)
	expRows, err := s.db.QueryContext(ctx,
		`SELECT sd.name, si.expiration_date, st.name
		 FROM supply_instances si
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON si.study_id = st.id
		 LEFT JOIN current_stock cs ON si.id = cs.supply_instance_id AND si.study_id = cs.study_id
		 WHERE si.active = 1
		   AND si.expiration_date IS NOT NULL
		   AND COALESCE(cs.quantity, 0) > 0`,
	)
	if err != nil {
		return nil, fmt.Errorf("checking expiration alerts: %w", err)
	}
	defer expRows.Close()

	now := time.Now()
	for expRows.Next() {
		var name, expDate, studyName string
		if err := expRows.Scan(&name, &expDate, &studyName); err != nil {
			return nil, fmt.Errorf("scanning expiration: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339, expDate)
		if err != nil {
			parsed, err = time.Parse("2006-01-02", expDate)
		}
		if err != nil {
			continue
		}
		daysUntil := int(parsed.Sub(now).Hours() / 24)
		if daysUntil >= 60 {
			continue
		}
		a := models.StockAlert{
			SupplyName: name,
			StudyName:  studyName,
			AlertType:  "expiration",
		}
		if daysUntil < 0 {
			a.Severity = "expired"
			a.Detail = fmt.Sprintf("Vencido hace %d dias", -daysUntil)
		} else if daysUntil == 0 {
			a.Severity = "urgent"
			a.Detail = "Vence hoy"
		} else if daysUntil < 7 {
			a.Severity = "urgent"
			a.Detail = fmt.Sprintf("Vence en %d dias", daysUntil)
		} else {
			a.Severity = "warning"
			a.Detail = fmt.Sprintf("Vence en %d dias", daysUntil)
		}
		alerts = append(alerts, a)
	}

	return alerts, expRows.Err()
}

// GetActiveStudies returns active studies for form dropdowns.
func (s *StockService) GetActiveStudies(ctx context.Context) ([]models.Study, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, platform_1, platform_2, platform_3, active, created_at
		 FROM studies WHERE active = 1 ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing active studies: %w", err)
	}
	defer rows.Close()

	var studies []models.Study
	for rows.Next() {
		st, err := scanStudy(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning study: %w", err)
		}
		studies = append(studies, st)
	}
	return studies, rows.Err()
}

// GetActiveSupplyDefinitions returns active supply definitions for form dropdowns.
func (s *StockService) GetActiveSupplyDefinitions(ctx context.Context) ([]models.SupplyDefinition, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+supplyDefSelectCols+`, st.name
		 FROM supply_definitions sd
		 JOIN studies st ON sd.study_id = st.id
		 WHERE sd.active = 1
		 ORDER BY sd.name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing active supply definitions: %w", err)
	}
	defer rows.Close()

	var defs []models.SupplyDefinition
	for rows.Next() {
		sd, err := scanSupplyDefinition(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning supply definition: %w", err)
		}
		defs = append(defs, sd)
	}
	return defs, rows.Err()
}

// GetActiveSupplyInstances returns active supply instances for form dropdowns.
func (s *StockService) GetActiveSupplyInstances(ctx context.Context) ([]models.SupplyInstance, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT si.id, si.supply_definition_id, si.study_id, si.quantity,
		        si.expiration_date, si.lot_number, si.reorder_alert_quantity, si.expiration_alert_days,
		        si.received_date, si.notes, si.active, si.created_at, si.updated_at,
		        sd.name, st.name, sd.type
		 FROM supply_instances si
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON si.study_id = st.id
		 WHERE si.active = 1
		 ORDER BY sd.name, si.lot_number ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing active supply instances: %w", err)
	}
	defer rows.Close()

	var instances []models.SupplyInstance
	for rows.Next() {
		si, err := scanSupplyInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning supply instance: %w", err)
		}
		instances = append(instances, si)
	}
	return instances, rows.Err()
}

type CreateSupplyInstanceInput struct {
	SupplyDefinitionID   string
	StudyID              string
	Quantity             int
	ExpirationDate       string
	LotNumber            string
	ReorderAlertQuantity string
	ExpirationAlertDays  string
	Notes                string
	SerialNumbers        []string
}

// CreateSupplyInstance creates a new supply instance with optional serial numbers.
func (s *StockService) CreateSupplyInstance(ctx context.Context, input CreateSupplyInstanceInput, userID string) (*models.SupplyInstance, error) {
	if input.SupplyDefinitionID == "" {
		return nil, ErrSupplyRequired
	}
	if input.StudyID == "" {
		return nil, ErrStudyRequired
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Get supply definition type to determine if returnable items should be created
	var defType string
	err = tx.QueryRowContext(ctx,
		"SELECT type FROM supply_definitions WHERE id = ?",
		input.SupplyDefinitionID,
	).Scan(&defType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSupplyRequired
		}
		return nil, fmt.Errorf("checking supply definition type: %w", err)
	}

	id := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO supply_instances (id, supply_definition_id, study_id, quantity,
		 expiration_date, lot_number, reorder_alert_quantity, expiration_alert_days,
		 received_by, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, input.SupplyDefinitionID, input.StudyID, input.Quantity,
		nullIfEmpty(input.ExpirationDate), nullIfEmpty(input.LotNumber),
		nullIfEmpty(input.ReorderAlertQuantity), nullIfEmpty(input.ExpirationAlertDays),
		userID, nullIfEmpty(input.Notes),
	)
	if err != nil {
		return nil, fmt.Errorf("inserting supply instance: %w", err)
	}

	// Insert serial numbers if provided
	for _, sn := range input.SerialNumbers {
		sn = strings.TrimSpace(sn)
		if sn == "" {
			continue
		}
		snID := uuid.New().String()
		_, err = tx.ExecContext(ctx,
			`INSERT INTO supply_instance_serial_numbers (id, supply_instance_id, serial_number)
			 VALUES (?, ?, ?)`,
			snID, id, sn,
		)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return nil, ErrSerialNumberExists
			}
			return nil, fmt.Errorf("inserting serial number: %w", err)
		}

		// Create returnable_items entry for returnable supplies
		if defType == models.SupplyTypeReturnable {
			riID := uuid.New().String()
			_, err = tx.ExecContext(ctx,
				`INSERT INTO returnable_items (id, supply_instance_id, study_id, serial_number, status)
				 VALUES (?, ?, ?, ?, ?)`,
				riID, id, input.StudyID, sn, models.ItemStatusNotDelivered,
			)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE constraint failed") {
					return nil, ErrSerialNumberExists
				}
				return nil, fmt.Errorf("inserting returnable item: %w", err)
			}
		}
	}

	// Create current_stock entry
	csID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO current_stock (id, supply_instance_id, study_id, quantity, last_updated)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(supply_instance_id, study_id) DO UPDATE SET
		   quantity = quantity + ?,
		   last_updated = CURRENT_TIMESTAMP`,
		csID, id, input.StudyID, input.Quantity, input.Quantity,
	)
	if err != nil {
		return nil, fmt.Errorf("creating current stock: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Re-fetch with JOINs
	si, err := scanSupplyInstance(s.db.QueryRowContext(ctx,
		`SELECT si.id, si.supply_definition_id, si.study_id, si.quantity,
		        si.expiration_date, si.lot_number, si.reorder_alert_quantity, si.expiration_alert_days,
		        si.received_date, si.notes, si.active, si.created_at, si.updated_at,
		        sd.name, st.name, sd.type
		 FROM supply_instances si
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON si.study_id = st.id
		 WHERE si.id = ?`, id,
	))
	if err != nil {
		return nil, fmt.Errorf("re-fetching supply instance: %w", err)
	}
	return &si, nil
}

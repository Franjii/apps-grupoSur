package services

import (
	"database/sql"

	"inventory-webapp/internal/models"
)

type scanner interface {
	Scan(dest ...any) error
}

// scanUser scans all user fields including password_hash.
// Query must select: id, username, password_hash, name, email, role, active, created_at
func scanUser(row scanner) (models.User, error) {
	var u models.User
	var emailNull sql.NullString
	var usernameNull sql.NullString
	if err := row.Scan(&u.ID, &usernameNull, &u.PasswordHash, &u.FullName,
		&emailNull, &u.Role, &u.Active, &u.CreatedAt); err != nil {
		return u, err
	}
	if usernameNull.Valid {
		u.Username = usernameNull.String
	}
	if emailNull.Valid {
		u.Email = &emailNull.String
	}
	return u, nil
}

// scanUserSafe scans all user fields EXCEPT password_hash.
// Query must select: id, username, name, email, role, active, created_at
func scanUserSafe(row scanner) (models.User, error) {
	var u models.User
	var emailNull sql.NullString
	var usernameNull sql.NullString
	if err := row.Scan(&u.ID, &usernameNull, &u.FullName,
		&emailNull, &u.Role, &u.Active, &u.CreatedAt); err != nil {
		return u, err
	}
	if usernameNull.Valid {
		u.Username = usernameNull.String
	}
	if emailNull.Valid {
		u.Email = &emailNull.String
	}
	return u, nil
}

// scanStudy scans: id, name, platform_1, platform_2, platform_3, active, created_at
func scanStudy(row scanner) (models.Study, error) {
	var st models.Study
	var p1, p2, p3 sql.NullString
	if err := row.Scan(&st.ID, &st.Name, &p1, &p2, &p3, &st.Active, &st.CreatedAt); err != nil {
		return st, err
	}
	if p1.Valid {
		st.Platform1 = &p1.String
	}
	if p2.Valid {
		st.Platform2 = &p2.String
	}
	if p3.Valid {
		st.Platform3 = &p3.String
	}
	return st, nil
}

// scanSupplyDefinition scans: id, name, type, study_id, has_expiration_date, has_serial_number,
// expiration_alert_enabled, active, created_at, updated_at, study_name
func scanSupplyDefinition(row scanner) (models.SupplyDefinition, error) {
	var sd models.SupplyDefinition
	if err := row.Scan(&sd.ID, &sd.Name, &sd.Type, &sd.StudyID,
		&sd.HasExpirationDate, &sd.HasSerialNumber,
		&sd.ExpirationAlertEnabled,
		&sd.Active, &sd.CreatedAt, &sd.UpdatedAt, &sd.StudyName); err != nil {
		return sd, err
	}
	return sd, nil
}

// scanSupplyDefinitionNoJoin scans without the StudyName join field.
// Scans: id, name, type, study_id, has_expiration_date, has_serial_number,
// expiration_alert_enabled, active, created_at, updated_at
func scanSupplyDefinitionNoJoin(row scanner) (models.SupplyDefinition, error) {
	var sd models.SupplyDefinition
	if err := row.Scan(&sd.ID, &sd.Name, &sd.Type, &sd.StudyID,
		&sd.HasExpirationDate, &sd.HasSerialNumber,
		&sd.ExpirationAlertEnabled,
		&sd.Active, &sd.CreatedAt, &sd.UpdatedAt); err != nil {
		return sd, err
	}
	return sd, nil
}

// scanSupplyInstance scans: id, supply_definition_id, study_id, quantity,
// expiration_date, lot_number, reorder_alert_quantity, expiration_alert_days,
// received_date, notes, active, created_at, updated_at, supply_name, study_name, supply_type
func scanSupplyInstance(row scanner) (models.SupplyInstance, error) {
	var si models.SupplyInstance
	var expDate, lotNum, notes sql.NullString
	var reorderQty, expDays sql.NullInt64
	if err := row.Scan(&si.ID, &si.SupplyDefinitionID, &si.StudyID, &si.Quantity,
		&expDate, &lotNum, &reorderQty, &expDays,
		&si.ReceivedDate, &notes, &si.Active, &si.CreatedAt, &si.UpdatedAt,
		&si.SupplyName, &si.StudyName, &si.SupplyType); err != nil {
		return si, err
	}
	if expDate.Valid {
		si.ExpirationDate = &expDate.String
	}
	if lotNum.Valid {
		si.LotNumber = &lotNum.String
	}
	if reorderQty.Valid {
		v := int(reorderQty.Int64)
		si.ReorderAlertQuantity = &v
	}
	if expDays.Valid {
		v := int(expDays.Int64)
		si.ExpirationAlertDays = &v
	}
	if notes.Valid {
		si.Notes = &notes.String
	}
	return si, nil
}

// scanStockOperationListItem scans: id, study_name, supply_name, supply_type,
// operation, quantity, notes, performed_by_name, operation_date
func scanStockOperationListItem(row scanner) (models.StockOperationListItem, error) {
	var item models.StockOperationListItem
	var notesNull, performerNull sql.NullString
	if err := row.Scan(&item.ID, &item.StudyName, &item.SupplyName, &item.SupplyType,
		&item.Operation, &item.Quantity, &notesNull, &performerNull,
		&item.OperationDate); err != nil {
		return item, err
	}
	if notesNull.Valid {
		item.Notes = &notesNull.String
	}
	if performerNull.Valid {
		item.PerformedByName = &performerNull.String
	}
	return item, nil
}

// scanCurrentStockItem scans: supply_instance_id, supply_name, supply_type,
// study_id, study_name, quantity, received_date, serial_numbers, expiration_date
func scanCurrentStockItem(row scanner) (models.CurrentStockItem, error) {
	var item models.CurrentStockItem
	var serialNull, expNull sql.NullString
	if err := row.Scan(&item.SupplyInstanceID, &item.SupplyName, &item.SupplyType,
		&item.StudyID, &item.StudyName, &item.Quantity, &item.ReceivedDate,
		&serialNull, &expNull); err != nil {
		return item, err
	}
	if serialNull.Valid {
		item.SerialNumbers = serialNull.String
	}
	if expNull.Valid {
		item.ExpirationDate = &expNull.String
	}
	return item, nil
}

// scanReturnableItemListItem scans: id, supply_name, serial_number, status,
// current_participant_id, study_name, updated_at
func scanReturnableItemListItem(row scanner) (models.ReturnableItemListItem, error) {
	var item models.ReturnableItemListItem
	var participantNull sql.NullString
	if err := row.Scan(&item.ID, &item.SupplyName, &item.SerialNumber,
		&item.Status, &participantNull, &item.StudyName, &item.UpdatedAt); err != nil {
		return item, err
	}
	if participantNull.Valid {
		item.CurrentParticipantID = &participantNull.String
	}
	return item, nil
}

// scanDeliveryLogListItem scans: id, participant_id, supply_name, serial_number,
// operation, performed_by_name, operation_date
func scanDeliveryLogListItem(row scanner) (models.DeliveryLogListItem, error) {
	var item models.DeliveryLogListItem
	var performerNull sql.NullString
	if err := row.Scan(&item.ID, &item.ParticipantID, &item.SupplyName,
		&item.SerialNumber, &item.Operation, &performerNull,
		&item.OperationDate); err != nil {
		return item, err
	}
	if performerNull.Valid {
		item.PerformedByName = &performerNull.String
	}
	return item, nil
}

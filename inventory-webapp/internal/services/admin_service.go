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

type AdminService struct {
	db *sql.DB
}

func NewAdminService(db *sql.DB) *AdminService {
	return &AdminService{db: db}
}

// --- Studies CRUD ---

type CreateStudyInput struct {
	Name      string
	Platform1 string
	Platform2 string
	Platform3 string
}

type UpdateStudyInput struct {
	Name      string
	Platform1 string
	Platform2 string
	Platform3 string
}

const studySelectCols = `id, name, platform_1, platform_2, platform_3, active, created_at`

// ListStudies returns all studies (active and inactive), ordered by name.
func (s *AdminService) ListStudies(ctx context.Context) ([]models.Study, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+studySelectCols+` FROM studies ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing studies: %w", err)
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

// GetStudyByID returns a study by ID without filtering by active status.
func (s *AdminService) GetStudyByID(ctx context.Context, id string) (*models.Study, error) {
	st, err := scanStudy(s.db.QueryRowContext(ctx,
		`SELECT `+studySelectCols+` FROM studies WHERE id = ?`, id,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStudyNotFound
		}
		return nil, fmt.Errorf("getting study: %w", err)
	}
	return &st, nil
}

// nullIfEmpty returns nil for empty strings, otherwise the trimmed string.
func nullIfEmpty(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

// CreateStudy creates a new study with unique name validation.
func (s *AdminService) CreateStudy(ctx context.Context, input CreateStudyInput) (*models.Study, error) {
	input.Name = strings.TrimSpace(input.Name)

	if input.Name == "" {
		return nil, ErrStudyNameRequired
	}
	if len(input.Name) > MaxNameLength {
		return nil, ErrNameTooLong
	}

	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM studies WHERE name = ?", input.Name,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("checking study name: %w", err)
	}
	if count > 0 {
		return nil, ErrStudyNameExists
	}

	id := uuid.New().String()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO studies (id, name, platform_1, platform_2, platform_3) VALUES (?, ?, ?, ?, ?)`,
		id, input.Name, nullIfEmpty(input.Platform1), nullIfEmpty(input.Platform2), nullIfEmpty(input.Platform3),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrStudyNameExists
		}
		return nil, fmt.Errorf("inserting study: %w", err)
	}

	return s.GetStudyByID(ctx, id)
}

// UpdateStudy updates a study's name and platforms with unique name validation.
func (s *AdminService) UpdateStudy(ctx context.Context, id string, input UpdateStudyInput) (*models.Study, error) {
	input.Name = strings.TrimSpace(input.Name)

	if input.Name == "" {
		return nil, ErrStudyNameRequired
	}
	if len(input.Name) > MaxNameLength {
		return nil, ErrNameTooLong
	}

	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM studies WHERE name = ? AND id != ?", input.Name, id,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("checking study name: %w", err)
	}
	if count > 0 {
		return nil, ErrStudyNameExists
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE studies SET name = ?, platform_1 = ?, platform_2 = ?, platform_3 = ? WHERE id = ?`,
		input.Name, nullIfEmpty(input.Platform1), nullIfEmpty(input.Platform2), nullIfEmpty(input.Platform3), id,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrStudyNameExists
		}
		return nil, fmt.Errorf("updating study: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrStudyNotFound
	}

	return s.GetStudyByID(ctx, id)
}

// DeactivateStudy soft-deletes a study.
func (s *AdminService) DeactivateStudy(ctx context.Context, id string) (*models.Study, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE studies SET active = 0 WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("deactivating study: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrStudyNotFound
	}

	return s.GetStudyByID(ctx, id)
}

// ActivateStudy reactivates a previously deactivated study.
func (s *AdminService) ActivateStudy(ctx context.Context, id string) (*models.Study, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE studies SET active = 1 WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("activating study: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrStudyNotFound
	}

	return s.GetStudyByID(ctx, id)
}

// --- Supply Definitions CRUD ---

type CreateSupplyDefinitionInput struct {
	Name                   string
	Type                   string
	StudyID                string
	HasExpirationDate      bool
	HasSerialNumber        bool
	ExpirationAlertEnabled bool
}

type UpdateSupplyDefinitionInput struct {
	Name                   string
	Type                   string
	StudyID                string
	HasExpirationDate      bool
	HasSerialNumber        bool
	ExpirationAlertEnabled bool
}

var validSupplyTypes = map[string]bool{
	models.SupplyTypeSingleUse:  true,
	models.SupplyTypeReturnable: true,
	models.SupplyTypeDisposable: true,
}

const supplyDefSelectCols = `sd.id, sd.name, sd.type, sd.study_id, sd.has_expiration_date, sd.has_serial_number,
	sd.expiration_alert_enabled, sd.active, sd.created_at, sd.updated_at`

// ListSupplyDefinitions returns all supply definitions with study names, ordered by name.
func (s *AdminService) ListSupplyDefinitions(ctx context.Context) ([]models.SupplyDefinition, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+supplyDefSelectCols+`, st.name
		 FROM supply_definitions sd
		 JOIN studies st ON sd.study_id = st.id
		 ORDER BY sd.name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing supply definitions: %w", err)
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

// GetSupplyDefinitionByID returns a supply definition by ID.
func (s *AdminService) GetSupplyDefinitionByID(ctx context.Context, id string) (*models.SupplyDefinition, error) {
	sd, err := scanSupplyDefinition(s.db.QueryRowContext(ctx,
		`SELECT `+supplyDefSelectCols+`, st.name
		 FROM supply_definitions sd
		 JOIN studies st ON sd.study_id = st.id
		 WHERE sd.id = ?`, id,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSupplyDefinitionNotFound
		}
		return nil, fmt.Errorf("getting supply definition: %w", err)
	}
	return &sd, nil
}

// CreateSupplyDefinition creates a new supply definition.
func (s *AdminService) CreateSupplyDefinition(ctx context.Context, input CreateSupplyDefinitionInput) (*models.SupplyDefinition, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Type = strings.TrimSpace(input.Type)

	if input.Name == "" {
		return nil, ErrSupplyDefinitionNameRequired
	}
	if len(input.Name) > MaxNameLength {
		return nil, ErrNameTooLong
	}
	if !validSupplyTypes[input.Type] {
		return nil, ErrInvalidSupplyType
	}
	if input.StudyID == "" {
		return nil, ErrStudyRequired
	}

	// Check name uniqueness within same study
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM supply_definitions WHERE name = ? AND study_id = ?", input.Name, input.StudyID,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("checking supply definition name: %w", err)
	}
	if count > 0 {
		return nil, ErrSupplyDefinitionNameExists
	}

	id := uuid.New().String()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO supply_definitions (id, name, type, study_id, has_expiration_date, has_serial_number,
		 expiration_alert_enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, input.Name, input.Type, input.StudyID,
		input.HasExpirationDate, input.HasSerialNumber,
		input.ExpirationAlertEnabled,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting supply definition: %w", err)
	}

	return s.GetSupplyDefinitionByID(ctx, id)
}

// UpdateSupplyDefinition updates a supply definition.
func (s *AdminService) UpdateSupplyDefinition(ctx context.Context, id string, input UpdateSupplyDefinitionInput) (*models.SupplyDefinition, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Type = strings.TrimSpace(input.Type)

	if input.Name == "" {
		return nil, ErrSupplyDefinitionNameRequired
	}
	if len(input.Name) > MaxNameLength {
		return nil, ErrNameTooLong
	}
	if !validSupplyTypes[input.Type] {
		return nil, ErrInvalidSupplyType
	}
	if input.StudyID == "" {
		return nil, ErrStudyRequired
	}

	// Check name uniqueness within same study, excluding current record
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM supply_definitions WHERE name = ? AND study_id = ? AND id != ?",
		input.Name, input.StudyID, id,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("checking supply definition name: %w", err)
	}
	if count > 0 {
		return nil, ErrSupplyDefinitionNameExists
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE supply_definitions SET name = ?, type = ?, study_id = ?,
		 has_expiration_date = ?, has_serial_number = ?,
		 expiration_alert_enabled = ?,
		 updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		input.Name, input.Type, input.StudyID,
		input.HasExpirationDate, input.HasSerialNumber,
		input.ExpirationAlertEnabled,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating supply definition: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrSupplyDefinitionNotFound
	}

	return s.GetSupplyDefinitionByID(ctx, id)
}

// DeactivateSupplyDefinition soft-deletes a supply definition.
func (s *AdminService) DeactivateSupplyDefinition(ctx context.Context, id string) (*models.SupplyDefinition, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE supply_definitions SET active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("deactivating supply definition: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrSupplyDefinitionNotFound
	}

	return s.GetSupplyDefinitionByID(ctx, id)
}

// ActivateSupplyDefinition reactivates a supply definition.
func (s *AdminService) ActivateSupplyDefinition(ctx context.Context, id string) (*models.SupplyDefinition, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE supply_definitions SET active = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("activating supply definition: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return nil, ErrSupplyDefinitionNotFound
	}

	return s.GetSupplyDefinitionByID(ctx, id)
}

package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type ExpirationAlertService struct {
	db           *sql.DB
	emailService *EmailService
}

func NewExpirationAlertService(db *sql.DB, emailService *EmailService) *ExpirationAlertService {
	return &ExpirationAlertService{db: db, emailService: emailService}
}

type milestoneDefinition struct {
	Key      string // DB value
	MaxDays  int    // Upper bound (inclusive) for triggering
	Label    string // Spanish label for email
	Severity string // For email color coding
}

var milestones = []milestoneDefinition{
	{"expired", 0, "Vencido", "expired"},
	{"1_week", 7, "Vence en 1 semana", "urgent"},
	{"1_month", 30, "Vence en 1 mes", "warning"},
	{"2_months", 60, "Vence en 2 meses", "info"},
}

// CheckAndNotify checks for supply instances approaching expiration and sends
// digest email notifications to all active users. Each milestone is sent only once
// per supply instance (deduplicated via DB tracking table).
func (s *ExpirationAlertService) CheckAndNotify(ctx context.Context) error {
	if !s.emailService.IsConfigured() {
		slog.Debug("Expiration alert check skipped (SMTP not configured)")
		return nil
	}

	// Query active supply instances with expiration dates, joined with active definitions
	// that have expiration_alert_enabled, and with stock > 0
	rows, err := s.db.QueryContext(ctx,
		`SELECT si.id, sd.name, st.name, COALESCE(si.lot_number, ''), si.expiration_date
		 FROM supply_instances si
		 JOIN supply_definitions sd ON si.supply_definition_id = sd.id
		 JOIN studies st ON si.study_id = st.id
		 LEFT JOIN current_stock cs ON si.id = cs.supply_instance_id AND si.study_id = cs.study_id
		 WHERE si.active = 1 AND sd.active = 1 AND sd.expiration_alert_enabled = 1
		   AND si.expiration_date IS NOT NULL AND COALESCE(cs.quantity, 0) > 0`,
	)
	if err != nil {
		return fmt.Errorf("querying expiring supply instances: %w", err)
	}
	defer rows.Close()

	now := time.Now().Truncate(24 * time.Hour)

	// Collect all instances into memory first, so we can close the rows
	// before doing INSERT queries (avoids holding a connection during writes).
	type expiringInstance struct {
		id, supplyName, studyName, lotNumber string
		daysUntil                            int
		formattedDate                        string
	}
	var instances []expiringInstance

	for rows.Next() {
		var instanceID, supplyName, studyName, lotNumber, expDateStr string
		if err := rows.Scan(&instanceID, &supplyName, &studyName, &lotNumber, &expDateStr); err != nil {
			return fmt.Errorf("scanning expiring instance: %w", err)
		}

		parsed, err := time.Parse("2006-01-02", expDateStr)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, expDateStr)
		}
		if err != nil {
			slog.Warn("Skipping instance with unparseable expiration date",
				"instance_id", instanceID, "date", expDateStr)
			continue
		}

		instances = append(instances, expiringInstance{
			id:            instanceID,
			supplyName:    supplyName,
			studyName:     studyName,
			lotNumber:     lotNumber,
			daysUntil:     int(parsed.Sub(now).Hours() / 24),
			formattedDate: parsed.Format("02/01/2006"),
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating expiring instances: %w", err)
	}
	rows.Close() // Close explicitly before doing writes

	var pendingAlerts []ExpirationAlertItem
	for _, inst := range instances {
		for _, m := range milestones {
			if inst.daysUntil > m.MaxDays {
				continue
			}

			result, err := s.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO expiration_notifications_sent (id, supply_instance_id, milestone)
				 VALUES (?, ?, ?)`,
				uuid.New().String(), inst.id, m.Key,
			)
			if err != nil {
				return fmt.Errorf("recording milestone %s for instance %s: %w", m.Key, inst.id, err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("checking rows affected: %w", err)
			}
			if affected == 0 {
				continue
			}

			pendingAlerts = append(pendingAlerts, ExpirationAlertItem{
				SupplyName:     inst.supplyName,
				StudyName:      inst.studyName,
				LotNumber:      inst.lotNumber,
				ExpirationDate: inst.formattedDate,
				MilestoneLabel: m.Label,
				Severity:       m.Severity,
			})
		}
	}

	if len(pendingAlerts) == 0 {
		slog.Info("Expiration alert check completed, no new alerts")
		return nil
	}

	slog.Info("Expiration alert check found new alerts", "count", len(pendingAlerts))

	// Query active users with emails
	userRows, err := s.db.QueryContext(ctx,
		`SELECT name, email FROM users WHERE active = 1 AND email IS NOT NULL AND email != ''`,
	)
	if err != nil {
		return fmt.Errorf("querying active users: %w", err)
	}
	defer userRows.Close()

	type userInfo struct {
		Name  string
		Email string
	}
	var users []userInfo
	for userRows.Next() {
		var u userInfo
		if err := userRows.Scan(&u.Name, &u.Email); err != nil {
			return fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, u)
	}
	if err := userRows.Err(); err != nil {
		return fmt.Errorf("iterating users: %w", err)
	}
	userRows.Close() // Close explicitly before sending emails

	if len(users) == 0 {
		slog.Info("Expiration alert check: no active users with emails to notify")
		return nil
	}

	// Send one digest email per user
	var sendErrors int
	for _, u := range users {
		if err := s.emailService.SendExpirationAlertEmail(u.Email, u.Name, pendingAlerts); err != nil {
			slog.Error("Failed to send expiration alert email", "to", u.Email, "error", err)
			sendErrors++
		}
	}

	slog.Info("Expiration alert emails sent",
		"users", len(users), "alerts", len(pendingAlerts), "errors", sendErrors)
	return nil
}

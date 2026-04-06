package runners

import (
	"context"
	"fmt"

	"github.com/chukuago/api/pkg/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the runners data access layer.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// GetProfile returns the runner profile for the given user, or nil if not found.
func (s *Store) GetProfile(ctx context.Context, userID uuid.UUID) (*types.RunnerProfile, error) {
	const query = `
		SELECT user_id, bio, kyc_status, vehicle_type, vehicle_plate,
		       is_available, rating_avg, rating_count, completed_count, cancelled_count,
		       created_at, updated_at
		FROM runner_profiles WHERE user_id = $1
	`
	var p types.RunnerProfile
	err := s.db.QueryRow(ctx, query, userID).Scan(
		&p.UserID, &p.Bio, &p.KYCStatus, &p.VehicleType, &p.VehiclePlate,
		&p.IsAvailable, &p.RatingAvg, &p.RatingCount, &p.CompletedCount, &p.CancelledCount,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runner profile: %w", err)
	}
	return &p, nil
}

// EnsureProfile creates the runner_profiles row if it doesn't already exist.
func (s *Store) EnsureProfile(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO runner_profiles (user_id, kyc_status, is_available, rating_avg, rating_count, completed_count, cancelled_count, created_at, updated_at)
		 VALUES ($1, 'unsubmitted', false, 0, 0, 0, 0, NOW(), NOW())
		 ON CONFLICT (user_id) DO NOTHING`,
		userID,
	)
	return err
}

// SubmitKYC inserts the KYC document records and flips the profile status to 'pending'.
// Both operations happen inside a single transaction.
func (s *Store) SubmitKYC(ctx context.Context, userID uuid.UUID, req SubmitKYCRequest) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	docs := []struct {
		docType string
		url     string
	}{
		{"national_id_front", req.NationalIDFrontURL},
		{"national_id_back", req.NationalIDBackURL},
		{"selfie", req.SelfieURL},
	}

	for _, doc := range docs {
		_, err := tx.Exec(ctx,
			`INSERT INTO runner_kyc_documents (id, user_id, doc_type, doc_url, status, created_at)
			 VALUES ($1, $2, $3, $4, 'pending', NOW())`,
			uuid.New(), userID, doc.docType, doc.url,
		)
		if err != nil {
			return fmt.Errorf("insert kyc doc (%s): %w", doc.docType, err)
		}
	}

	_, err = tx.Exec(ctx,
		`UPDATE runner_profiles SET kyc_status = 'pending', updated_at = NOW() WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("update kyc_status: %w", err)
	}

	return tx.Commit(ctx)
}

// GetKYCStatus returns the latest KYC document status and any admin notes.
func (s *Store) GetKYCStatus(ctx context.Context, userID uuid.UUID) (*types.RunnerKYCDocument, error) {
	const query = `
		SELECT id, user_id, doc_type, doc_url, status, notes, created_at, reviewed_at, reviewed_by
		FROM runner_kyc_documents
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`
	var doc types.RunnerKYCDocument
	err := s.db.QueryRow(ctx, query, userID).Scan(
		&doc.ID, &doc.UserID, &doc.DocType, &doc.DocURL,
		&doc.Status, &doc.Notes, &doc.CreatedAt, &doc.ReviewedAt, &doc.ReviewedBy,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get kyc status: %w", err)
	}
	return &doc, nil
}

// SetAvailability flips the is_available flag on the runner's profile.
func (s *Store) SetAvailability(ctx context.Context, userID uuid.UUID, available bool) error {
	_, err := s.db.Exec(ctx,
		`UPDATE runner_profiles SET is_available = $2, updated_at = NOW() WHERE user_id = $1`,
		userID, available,
	)
	return err
}

// AddServiceArea links a region or custom zone to the runner.
func (s *Store) AddServiceArea(ctx context.Context, userID uuid.UUID, req AddServiceAreaRequest) (*types.RunnerServiceArea, error) {
	id := uuid.New()
	_, err := s.db.Exec(ctx,
		`INSERT INTO runner_service_areas (id, user_id, region_id, label, created_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		id, userID, req.RegionID, req.Label,
	)
	if err != nil {
		return nil, fmt.Errorf("insert service area: %w", err)
	}

	area := &types.RunnerServiceArea{ID: id, UserID: userID, Label: req.Label}
	if req.RegionID != nil {
		rid, _ := uuid.Parse(*req.RegionID)
		area.RegionID = &rid
	}
	return area, nil
}

// RemoveServiceArea deletes a runner's service area, enforcing ownership.
func (s *Store) RemoveServiceArea(ctx context.Context, userID uuid.UUID, areaID uuid.UUID) error {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM runner_service_areas WHERE id = $1 AND user_id = $2`,
		areaID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete service area: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAreaNotFound
	}
	return nil
}

// ListServiceAreas returns all service areas for a runner, joined with region name.
func (s *Store) ListServiceAreas(ctx context.Context, userID uuid.UUID) ([]ServiceAreaResponse, error) {
	const query = `
		SELECT rsa.id, rsa.region_id, sr.name, rsa.label
		FROM runner_service_areas rsa
		LEFT JOIN service_regions sr ON sr.id = rsa.region_id
		WHERE rsa.user_id = $1
		ORDER BY rsa.created_at
	`
	rows, err := s.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list service areas: %w", err)
	}
	defer rows.Close()

	var areas []ServiceAreaResponse
	for rows.Next() {
		var a ServiceAreaResponse
		var rid *uuid.UUID
		if err := rows.Scan(&a.ID, &rid, &a.RegionName, &a.Label); err != nil {
			return nil, fmt.Errorf("scan service area: %w", err)
		}
		if rid != nil {
			s := rid.String()
			a.RegionID = &s
		}
		areas = append(areas, a)
	}
	return areas, rows.Err()
}

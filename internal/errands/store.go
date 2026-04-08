package errands

import (
	"context"
	"fmt"

	"github.com/chukuago/api/pkg/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the errands data access layer.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Create persists a new errand and its initial stops inside a single transaction.
func (s *Store) Create(ctx context.Context, clientID uuid.UUID, req CreateErrandRequest) (*types.Errand, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	id := uuid.New()
	var regionID *uuid.UUID
	if req.RegionID != nil {
		rid, _ := uuid.Parse(*req.RegionID)
		regionID = &rid
	}

	const insertErrand = `
		INSERT INTO errands (
			id, client_id, title, description, category, currency,
			allow_bids, instant_accept, budget_min, budget_max, fixed_price,
			scheduled_at, expires_at, status, region_id, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'posted',$14,NOW(),NOW()
		)
		RETURNING id, client_id, title, description, category, currency,
		          allow_bids, instant_accept, budget_min, budget_max, fixed_price,
		          scheduled_at, expires_at, status, assigned_runner_id, accepted_offer_id,
		          region_id, created_at, updated_at
	`

	var e types.Errand
	err = tx.QueryRow(ctx, insertErrand,
		id, clientID, req.Title, req.Description, req.Category, req.Currency,
		req.AllowBids, req.InstantAccept, req.BudgetMin, req.BudgetMax, req.FixedPrice,
		req.ScheduledAt, req.ExpiresAt, regionID,
	).Scan(
		&e.ID, &e.ClientID, &e.Title, &e.Description, &e.Category, &e.Currency,
		&e.AllowBids, &e.InstantAccept, &e.BudgetMin, &e.BudgetMax, &e.FixedPrice,
		&e.ScheduledAt, &e.ExpiresAt, &e.Status, &e.AssignedRunnerID, &e.AcceptedOfferID,
		&e.RegionID, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert errand: %w", err)
	}

	for i, stop := range req.Stops {
		_, err := tx.Exec(ctx,
			`INSERT INTO errand_stops (id, errand_id, seq, stop_type, address_label,
			 address_text, contact_name, contact_phone, lat, lng, instructions, created_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())`,
			uuid.New(), id, i+1, stop.StopType, stop.AddressLabel,
			stop.AddressText, stop.ContactName, stop.ContactPhone,
			stop.Lat, stop.Lng, stop.Instructions,
		)
		if err != nil {
			return nil, fmt.Errorf("insert stop %d: %w", i+1, err)
		}
	}

	// Emit an audit event.
	if err := insertEvent(ctx, tx, id, nil, "errand_posted", nil); err != nil {
		return nil, err
	}

	return &e, tx.Commit(ctx)
}

// GetByID fetches an errand with its stops.
// GetByID fetches an errand with its stops, including client and runner names.
func (s *Store) GetByID(ctx context.Context, id uuid.UUID) (*types.Errand, []types.ErrandStop, error) {
	const query = `
		SELECT
			e.id, e.client_id, e.title, e.description, e.category, e.currency,
			e.allow_bids, e.instant_accept, e.budget_min, e.budget_max, e.fixed_price,
			e.scheduled_at, e.expires_at, e.status, e.assigned_runner_id, e.accepted_offer_id,
			e.region_id, e.created_at, e.updated_at,
			COALESCE(c.name, '')  AS client_name,
			COALESCE(ru.name, '') AS runner_name
		FROM errands e
		JOIN users c  ON c.id  = e.client_id
		LEFT JOIN users ru ON ru.id = e.assigned_runner_id
		WHERE e.id = $1
	`
	var e types.Errand
	err := s.db.QueryRow(ctx, query, id).Scan(
		&e.ID, &e.ClientID, &e.Title, &e.Description, &e.Category, &e.Currency,
		&e.AllowBids, &e.InstantAccept, &e.BudgetMin, &e.BudgetMax, &e.FixedPrice,
		&e.ScheduledAt, &e.ExpiresAt, &e.Status, &e.AssignedRunnerID, &e.AcceptedOfferID,
		&e.RegionID, &e.CreatedAt, &e.UpdatedAt,
		&e.ClientName, &e.RunnerName,  // ← two new fields
	)
	if err == pgx.ErrNoRows {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get errand: %w", err)
	}

	stops, err := s.listStops(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	return &e, stops, nil
}
// ListForClient returns paginated errands owned by the given client.
func (s *Store) ListForClient(ctx context.Context, clientID uuid.UUID, status *string, page, limit int) ([]types.Errand, int, error) {
	offset := (page - 1) * limit

	var (
		rows interface{ Close() }
		err  error
		args []interface{}
		sql  string
	)

	if status != nil {
		sql = `SELECT id, client_id, title, description, category, currency,
			allow_bids, instant_accept, budget_min, budget_max, fixed_price,
			scheduled_at, expires_at, status, assigned_runner_id, accepted_offer_id,
			region_id, created_at, updated_at
			FROM errands WHERE client_id = $1 AND status = $2
			ORDER BY created_at DESC LIMIT $3 OFFSET $4`
		args = []interface{}{clientID, *status, limit, offset}
	} else {
		sql = `SELECT id, client_id, title, description, category, currency,
			allow_bids, instant_accept, budget_min, budget_max, fixed_price,
			scheduled_at, expires_at, status, assigned_runner_id, accepted_offer_id,
			region_id, created_at, updated_at
			FROM errands WHERE client_id = $1
			ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = []interface{}{clientID, limit, offset}
	}

	pgRows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list errands: %w", err)
	}
	defer pgRows.Close()
	rows = pgRows

	_ = rows
	var errands []types.Errand
	for pgRows.Next() {
		var e types.Errand
		if err := pgRows.Scan(
			&e.ID, &e.ClientID, &e.Title, &e.Description, &e.Category, &e.Currency,
			&e.AllowBids, &e.InstantAccept, &e.BudgetMin, &e.BudgetMax, &e.FixedPrice,
			&e.ScheduledAt, &e.ExpiresAt, &e.Status, &e.AssignedRunnerID, &e.AcceptedOfferID,
			&e.RegionID, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan errand: %w", err)
		}
		errands = append(errands, e)
	}

	var total int
	if status != nil {
		s.db.QueryRow(ctx, `SELECT COUNT(*) FROM errands WHERE client_id=$1 AND status=$2`, clientID, *status).Scan(&total) //nolint:errcheck
	} else {
		s.db.QueryRow(ctx, `SELECT COUNT(*) FROM errands WHERE client_id=$1`, clientID).Scan(&total) //nolint:errcheck
	}

	return errands, total, pgRows.Err()
}

// ListForRunner returns paginated errands assigned to (or completed by) a runner.
func (s *Store) ListForRunner(ctx context.Context, runnerID uuid.UUID, status *string, page, limit int) ([]types.Errand, int, error) {
    offset := (page - 1) * limit

    var sql string
    var args []interface{}

    if status != nil {
        sql = `SELECT id, client_id, title, description, category, currency,
                allow_bids, instant_accept, budget_min, budget_max, fixed_price,
                scheduled_at, expires_at, status, assigned_runner_id, accepted_offer_id,
                region_id, created_at, updated_at
               FROM errands
               WHERE assigned_runner_id = $1 AND status = $2
               ORDER BY updated_at DESC LIMIT $3 OFFSET $4`
        args = []interface{}{runnerID, *status, limit, offset}
    } else {
        sql = `SELECT id, client_id, title, description, category, currency,
                allow_bids, instant_accept, budget_min, budget_max, fixed_price,
                scheduled_at, expires_at, status, assigned_runner_id, accepted_offer_id,
                region_id, created_at, updated_at
               FROM errands
               WHERE assigned_runner_id = $1
               ORDER BY updated_at DESC LIMIT $2 OFFSET $3`
        args = []interface{}{runnerID, limit, offset}
    }

    pgRows, err := s.db.Query(ctx, sql, args...)
    if err != nil {
        return nil, 0, fmt.Errorf("list runner errands: %w", err)
    }
    defer pgRows.Close()

    var errands []types.Errand
    for pgRows.Next() {
        var e types.Errand
        if err := pgRows.Scan(
            &e.ID, &e.ClientID, &e.Title, &e.Description, &e.Category, &e.Currency,
            &e.AllowBids, &e.InstantAccept, &e.BudgetMin, &e.BudgetMax, &e.FixedPrice,
            &e.ScheduledAt, &e.ExpiresAt, &e.Status, &e.AssignedRunnerID, &e.AcceptedOfferID,
            &e.RegionID, &e.CreatedAt, &e.UpdatedAt,
        ); err != nil {
            return nil, 0, fmt.Errorf("scan runner errand: %w", err)
        }
        errands = append(errands, e)
    }

    var total int
    if status != nil {
        s.db.QueryRow(ctx, `SELECT COUNT(*) FROM errands WHERE assigned_runner_id=$1 AND status=$2`, runnerID, *status).Scan(&total)
    } else {
        s.db.QueryRow(ctx, `SELECT COUNT(*) FROM errands WHERE assigned_runner_id=$1`, runnerID).Scan(&total)
    }

    return errands, total, pgRows.Err()
}

// Feed returns available errands for runners, optionally filtered by radius using PostGIS.
func (s *Store) Feed(ctx context.Context, req ErrandFeedRequest) ([]types.Errand, int, error) {
	offset := (req.Page - 1) * req.Limit

	// Base query — PostGIS distance filter applied only when coordinates are provided.
	var (
		baseSQL string
		args    []interface{}
	)

	if req.Lat != nil && req.Lng != nil && req.RadiusKM != nil {
		// Filter errand stops within the radius using PostGIS ST_DWithin.
		baseSQL = `
			SELECT DISTINCT e.id, e.client_id, e.title, e.description, e.category, e.currency,
			       e.allow_bids, e.instant_accept, e.budget_min, e.budget_max, e.fixed_price,
			       e.scheduled_at, e.expires_at, e.status, e.assigned_runner_id, e.accepted_offer_id,
			       e.region_id, e.created_at, e.updated_at
			FROM errands e
			JOIN errand_stops es ON es.errand_id = e.id
			WHERE e.status IN ('posted','bidding')
			  AND ST_DWithin(
			        ST_MakePoint(es.lng, es.lat)::geography,
			        ST_MakePoint($1, $2)::geography,
			        $3 * 1000
			      )
			ORDER BY e.created_at DESC
			LIMIT $4 OFFSET $5
		`
		args = []interface{}{*req.Lng, *req.Lat, *req.RadiusKM, req.Limit, offset}
	} else {
		baseSQL = `
			SELECT id, client_id, title, description, category, currency,
			       allow_bids, instant_accept, budget_min, budget_max, fixed_price,
			       scheduled_at, expires_at, status, assigned_runner_id, accepted_offer_id,
			       region_id, created_at, updated_at
			FROM errands
			WHERE status IN ('posted','bidding')
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2
		`
		args = []interface{}{req.Limit, offset}
	}

	pgRows, err := s.db.Query(ctx, baseSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("feed query: %w", err)
	}
	defer pgRows.Close()

	var errands []types.Errand
	for pgRows.Next() {
		var e types.Errand
		if err := pgRows.Scan(
			&e.ID, &e.ClientID, &e.Title, &e.Description, &e.Category, &e.Currency,
			&e.AllowBids, &e.InstantAccept, &e.BudgetMin, &e.BudgetMax, &e.FixedPrice,
			&e.ScheduledAt, &e.ExpiresAt, &e.Status, &e.AssignedRunnerID, &e.AcceptedOfferID,
			&e.RegionID, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan feed errand: %w", err)
		}
		errands = append(errands, e)
	}

	return errands, len(errands), pgRows.Err()
}

// UpdateStatus advances the errand FSM. The caller must validate the transition first.
func (s *Store) UpdateStatus(ctx context.Context, id uuid.UUID, actorID uuid.UUID, to types.ErrandStatus, notes *string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx,
		`UPDATE errands SET status = $2, updated_at = NOW() WHERE id = $1`,
		id, to,
	)
	if err != nil {
		return fmt.Errorf("update errand status: %w", err)
	}

	if err := insertEvent(ctx, tx, id, &actorID, "status_changed:"+string(to), notes); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Cancel marks an errand as cancelled, enforcing that it hasn't been assigned yet.
func (s *Store) Cancel(ctx context.Context, id uuid.UUID, clientID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var status types.ErrandStatus
	err = tx.QueryRow(ctx,
		`SELECT status FROM errands WHERE id = $1 AND client_id = $2 FOR UPDATE`,
		id, clientID,
	).Scan(&status)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock errand: %w", err)
	}

	if !IsValidTransition(status, types.ErrandCancelled) {
		return ErrNotCancellable
	}

	_, err = tx.Exec(ctx,
		`UPDATE errands SET status = 'cancelled', updated_at = NOW() WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("cancel errand: %w", err)
	}

	if err := insertEvent(ctx, tx, id, &clientID, "errand_cancelled", nil); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// AddStop appends a new stop to an existing errand.
func (s *Store) AddStop(ctx context.Context, errandID uuid.UUID, req CreateStopRequest) (*types.ErrandStop, error) {
	var maxSeq int
	s.db.QueryRow(ctx, `SELECT COALESCE(MAX(seq),0) FROM errand_stops WHERE errand_id=$1`, errandID).Scan(&maxSeq) //nolint:errcheck

	id := uuid.New()
	_, err := s.db.Exec(ctx,
		`INSERT INTO errand_stops (id, errand_id, seq, stop_type, address_label,
		 address_text, contact_name, contact_phone, lat, lng, instructions, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())`,
		id, errandID, maxSeq+1, req.StopType, req.AddressLabel,
		req.AddressText, req.ContactName, req.ContactPhone,
		req.Lat, req.Lng, req.Instructions,
	)
	if err != nil {
		return nil, fmt.Errorf("add stop: %w", err)
	}

	return &types.ErrandStop{
		ID: id, ErrandID: errandID, Seq: maxSeq + 1,
		StopType: types.StopType(req.StopType),
	}, nil
}

func (s *Store) listStops(ctx context.Context, errandID uuid.UUID) ([]types.ErrandStop, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, errand_id, seq, stop_type, address_label, address_text,
		        contact_name, contact_phone, lat, lng, instructions, created_at
		 FROM errand_stops WHERE errand_id = $1 ORDER BY seq`,
		errandID,
	)
	if err != nil {
		return nil, fmt.Errorf("list stops: %w", err)
	}
	defer rows.Close()

	var stops []types.ErrandStop
	for rows.Next() {
		var st types.ErrandStop
		if err := rows.Scan(
			&st.ID, &st.ErrandID, &st.Seq, &st.StopType,
			&st.AddressLabel, &st.AddressText,
			&st.ContactName, &st.ContactPhone,
			&st.Lat, &st.Lng, &st.Instructions, &st.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan stop: %w", err)
		}
		stops = append(stops, st)
	}
	return stops, rows.Err()
}

// insertEvent appends an immutable event to errand_events (must run inside a tx).
func insertEvent(ctx context.Context, tx pgx.Tx, errandID uuid.UUID, actorID *uuid.UUID, eventType string, notes *string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO errand_events (id, errand_id, actor_user_id, event_type, notes, occurred_at)
		 VALUES ($1,$2,$3,$4,$5,NOW())`,
		uuid.New(), errandID, actorID, eventType, notes,
	)
	if err != nil {
		return fmt.Errorf("insert errand event: %w", err)
	}
	return nil
}

package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/chukuago/api/pkg/response"
	pkgtypes "github.com/chukuago/api/pkg/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	// firebase.google.com/go/v4 would be the production import.
	// For MVP compilation without the SDK dependency we keep the interface
	// and provide a stub — swap in the real SDK on first deploy.
	"golang.org/x/oauth2/google"
)

// ---- DTOs ------------------------------------------------------------------

type NotificationResponse struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
}

// ---- FCM Pusher ------------------------------------------------------------
// Firebase Cloud Messaging is the recommended push provider for ChukuaGo because:
//   - Free for the volumes ChukuaGo will see at MVP
//   - Flutter firebase_messaging package has first-class support
//   - Works on both Android (dominant in Kenya) and iOS
//   - Simple HTTP v1 API with service account credentials

type fcmMessage struct {
	Message struct {
		Token        string            `json:"token"`
		Notification fcmNotification   `json:"notification"`
		Data         map[string]string `json:"data,omitempty"`
	} `json:"message"`
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// FirebasePusher sends push notifications via the FCM HTTP v1 API.
type FirebasePusher struct {
	projectID string
	creds     []byte
	httpClient *http.Client
}

// NewFirebasePusher creates a FirebasePusher from a service account JSON credential string.
func NewFirebasePusher(credJSON string) *FirebasePusher {
	return &FirebasePusher{
		creds:      []byte(credJSON),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// SendPush dispatches a push notification to the device registered for userID.
// In a full implementation the FCM registration token would be stored per-user
// (e.g. updated from the app on each login). For MVP we look it up from a
// device_tokens table (not shown, trivial to add).
func (p *FirebasePusher) SendPush(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) error {
	// Obtain a short-lived Google OAuth2 access token using the service account.
	conf, err := google.CredentialsFromJSON(ctx, p.creds, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return fmt.Errorf("fcm credentials: %w", err)
	}
	tok, err := conf.TokenSource.Token()
	if err != nil {
		return fmt.Errorf("fcm token: %w", err)
	}

	// Look up the FCM device token for this user (stubbed — wire to DB in prod).
	deviceToken := lookupDeviceToken(ctx, userID)
	if deviceToken == "" {
		return nil // user has no registered device — silently skip
	}

	var msg fcmMessage
	msg.Message.Token = deviceToken
	msg.Message.Notification = fcmNotification{Title: title, Body: body}
	msg.Message.Data = data

	payload, _ := json.Marshal(msg)
	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", p.projectID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build fcm request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send fcm request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("fcm responded with status %d", resp.StatusCode)
	}

	return nil
}

// lookupDeviceToken is a placeholder — replace with a DB lookup against a
// device_tokens table that the Flutter app updates on each login.
func lookupDeviceToken(_ context.Context, _ uuid.UUID) string {
	// In production: SELECT fcm_token FROM device_tokens WHERE user_id=$1 ORDER BY updated_at DESC LIMIT 1
	return os.Getenv("FCM_DEV_TOKEN") // useful for local testing
}

// ---- Store -----------------------------------------------------------------

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Save persists an in-app notification record.
func (s *Store) Save(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) error {
	dataJSON, _ := json.Marshal(data)
	_, err := s.db.Exec(ctx,
		`INSERT INTO notifications (id, user_id, title, body, data, status, created_at)
		 VALUES ($1,$2,$3,$4,$5,'unread',NOW())`,
		uuid.New(), userID, title, body, dataJSON,
	)
	return err
}

// ListForUser returns unread + recent read notifications for the user.
func (s *Store) ListForUser(ctx context.Context, userID uuid.UUID, page, limit int) ([]pkgtypes.Notification, int, error) {
	offset := (page - 1) * limit
	rows, err := s.db.Query(ctx,
		`SELECT id, user_id, title, body, status, created_at, read_at
		 FROM notifications WHERE user_id=$1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	var notifs []pkgtypes.Notification
	for rows.Next() {
		var n pkgtypes.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Title, &n.Body, &n.Status, &n.CreatedAt, &n.ReadAt); err != nil {
			return nil, 0, fmt.Errorf("scan notification: %w", err)
		}
		notifs = append(notifs, n)
	}

	var total int
	s.db.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id=$1`, userID).Scan(&total) //nolint:errcheck

	return notifs, total, rows.Err()
}

// MarkRead marks a single notification as read.
func (s *Store) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE notifications SET status='read', read_at=NOW() WHERE id=$1 AND user_id=$2 AND status='unread'`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("notification not found or already read")
	}
	return nil
}

// ---- Service ---------------------------------------------------------------

type Pusher interface {
	SendPush(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) error
}

type Service struct {
	store  *Store
	pusher Pusher
	log    *zap.Logger
}

func NewService(store *Store, pusher Pusher, log *zap.Logger) *Service {
	return &Service{store: store, pusher: pusher, log: log}
}

// SendPush delivers a push notification and persists the in-app record.
// Errors from the push provider are logged but not propagated — we never want
// a notification failure to break the calling business operation.
func (s *Service) SendPush(ctx context.Context, userID uuid.UUID, title, body string, data map[string]string) error {
	if err := s.store.Save(ctx, userID, title, body, data); err != nil {
		s.log.Warn("failed to persist notification", zap.Error(err), zap.String("userId", userID.String()))
	}
	if err := s.pusher.SendPush(ctx, userID, title, body, data); err != nil {
		s.log.Warn("FCM push failed", zap.Error(err), zap.String("userId", userID.String()))
	}
	return nil
}

func (s *Service) List(ctx context.Context, page, limit int) ([]NotificationResponse, int, error) {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}

	notifs, total, err := s.store.ListForUser(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, err
	}

	resp := make([]NotificationResponse, len(notifs))
	for i, n := range notifs {
		resp[i] = NotificationResponse{
			ID:        n.ID.String(),
			Title:     n.Title,
			Body:      n.Body,
			Status:    string(n.Status),
			CreatedAt: n.CreatedAt,
			ReadAt:    n.ReadAt,
		}
	}
	return resp, total, nil
}

func (s *Service) MarkRead(ctx context.Context, notifID uuid.UUID) error {
	userID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return err
	}
	return s.store.MarkRead(ctx, notifID, userID)
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// List godoc
// GET /api/v1/notifications
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 30
	}

	notifs, total, err := h.svc.List(r.Context(), page, limit)
	if err != nil {
		h.log.Error("List notifications failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSONList(w, http.StatusOK, notifs, page, limit, total)
}

// MarkRead godoc
// PATCH /api/v1/notifications/:id/read
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "notification ID must be a valid UUID")
		return
	}

	if err := h.svc.MarkRead(r.Context(), id); err != nil {
		h.log.Error("MarkRead failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

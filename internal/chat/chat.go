package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/chukuago/api/pkg/response"
	pkgtypes "github.com/chukuago/api/pkg/types"
	"github.com/chukuago/api/pkg/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"golang.org/x/net/websocket"
)

// ---- DTOs ------------------------------------------------------------------

type SendMessageRequest struct {
	MessageType string  `json:"messageType" validate:"required,oneof=text image"`
	Content     *string `json:"content"     validate:"omitempty,max=4000"`
	Attachment  *string `json:"attachmentUrl" validate:"omitempty,url"`
}

type MessageResponse struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversationId"`
	SenderID       string     `json:"senderId"`
	MessageType    string     `json:"messageType"`
	Content        *string    `json:"content,omitempty"`
	AttachmentURL  *string    `json:"attachmentUrl,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	ReadAt         *time.Time `json:"readAt,omitempty"`
}

type ConversationResponse struct {
	ID        string            `json:"id"`
	ErrandID  string            `json:"errandId"`
	Messages  []MessageResponse `json:"messages"`
	CreatedAt time.Time         `json:"createdAt"`
}

// ---- Errors ----------------------------------------------------------------

var (
	ErrConvNotFound = errors.New("conversation not found")
	ErrNotInConv    = errors.New("you are not a participant in this conversation")
)

// ---- WebSocket Hub ---------------------------------------------------------

// client represents a single connected WebSocket peer.
type client struct {
	conversationID uuid.UUID
	userID         uuid.UUID
	conn           *websocket.Conn
	send           chan []byte
}

// Hub maintains active WebSocket connections keyed by conversation ID.
type Hub struct {
	mu      sync.RWMutex
	rooms   map[uuid.UUID]map[*client]struct{} // conversationID → clients
	register   chan *client
	unregister chan *client
	broadcast  chan broadcastMsg
}

type broadcastMsg struct {
	conversationID uuid.UUID
	payload        []byte
	sender         *client
}

// NewHub creates an idle Hub. Call Run() in a goroutine.
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[uuid.UUID]map[*client]struct{}),
		register:   make(chan *client, 64),
		unregister: make(chan *client, 64),
		broadcast:  make(chan broadcastMsg, 256),
	}
}

// Run starts the hub's event loop. Must be called in its own goroutine.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case c := <-h.register:
			h.mu.Lock()
			if _, ok := h.rooms[c.conversationID]; !ok {
				h.rooms[c.conversationID] = make(map[*client]struct{})
			}
			h.rooms[c.conversationID][c] = struct{}{}
			h.mu.Unlock()

		case c := <-h.unregister:
			h.mu.Lock()
			if room, ok := h.rooms[c.conversationID]; ok {
				delete(room, c)
				if len(room) == 0 {
					delete(h.rooms, c.conversationID)
				}
			}
			h.mu.Unlock()
			close(c.send)

		case msg := <-h.broadcast:
			h.mu.RLock()
			room := h.rooms[msg.conversationID]
			h.mu.RUnlock()

			for c := range room {
				if c == msg.sender {
					continue // don't echo back to sender
				}
				select {
				case c.send <- msg.payload:
				default:
					// Slow consumer — drop and disconnect.
					h.unregister <- c
				}
			}
		}
	}
}

// Broadcast fans a message out to all connected peers in the conversation.
func (h *Hub) Broadcast(conversationID uuid.UUID, payload []byte, sender *client) {
	h.broadcast <- broadcastMsg{conversationID: conversationID, payload: payload, sender: sender}
}

// ---- Store -----------------------------------------------------------------

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// GetOrCreateConversation returns the conversation for an errand, creating it if needed.
func (s *Store) GetOrCreateConversation(ctx context.Context, errandID uuid.UUID) (*pkgtypes.Conversation, error) {
	conv := &pkgtypes.Conversation{}
	err := s.db.QueryRow(ctx,
		`INSERT INTO conversations (id, errand_id, created_at) VALUES ($1,$2,NOW())
		 ON CONFLICT (errand_id) DO UPDATE SET errand_id = EXCLUDED.errand_id
		 RETURNING id, errand_id, created_at`,
		uuid.New(), errandID,
	).Scan(&conv.ID, &conv.ErrandID, &conv.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get or create conversation: %w", err)
	}
	return conv, nil
}

// GetConversationByErrand fetches the conversation and its messages.
func (s *Store) GetConversationByErrand(ctx context.Context, errandID uuid.UUID) (*pkgtypes.Conversation, []pkgtypes.Message, error) {
	var conv pkgtypes.Conversation
	err := s.db.QueryRow(ctx,
		`SELECT id, errand_id, created_at FROM conversations WHERE errand_id = $1`,
		errandID,
	).Scan(&conv.ID, &conv.ErrandID, &conv.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil, ErrConvNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get conversation: %w", err)
	}

	rows, err := s.db.Query(ctx,
		`SELECT id, conversation_id, sender_id, message_type, content, attachment_url, created_at, read_at
		 FROM messages WHERE conversation_id = $1 ORDER BY created_at ASC`,
		conv.ID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var msgs []pkgtypes.Message
	for rows.Next() {
		var m pkgtypes.Message
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.SenderID, &m.MessageType,
			&m.Content, &m.AttachmentURL, &m.CreatedAt, &m.ReadAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}

	return &conv, msgs, rows.Err()
}

// SaveMessage persists a chat message.
func (s *Store) SaveMessage(ctx context.Context, convID, senderID uuid.UUID, req SendMessageRequest) (*pkgtypes.Message, error) {
	var m pkgtypes.Message
	err := s.db.QueryRow(ctx,
		`INSERT INTO messages (id, conversation_id, sender_id, message_type, content, attachment_url, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,NOW())
		 RETURNING id, conversation_id, sender_id, message_type, content, attachment_url, created_at, read_at`,
		uuid.New(), convID, senderID, req.MessageType, req.Content, req.Attachment,
	).Scan(
		&m.ID, &m.ConversationID, &m.SenderID, &m.MessageType,
		&m.Content, &m.AttachmentURL, &m.CreatedAt, &m.ReadAt,
	)
	if err != nil {
		return nil, fmt.Errorf("save message: %w", err)
	}
	return &m, nil
}

// ---- Service ---------------------------------------------------------------

type Service struct {
	store *Store
	hub   *Hub
	log   *zap.Logger
}

func NewService(store *Store, log *zap.Logger) *Service {
	hub := NewHub()
	go hub.Run(context.Background())
	return &Service{store: store, hub: hub, log: log}
}

func (s *Service) GetConversation(ctx context.Context, errandID uuid.UUID) (*ConversationResponse, error) {
	conv, msgs, err := s.store.GetConversationByErrand(ctx, errandID)
	if err != nil {
		return nil, err
	}

	resp := &ConversationResponse{
		ID:        conv.ID.String(),
		ErrandID:  conv.ErrandID.String(),
		CreatedAt: conv.CreatedAt,
		Messages:  make([]MessageResponse, len(msgs)),
	}

	for i, m := range msgs {
		resp.Messages[i] = toMsgResponse(&m)
	}
	return resp, nil
}

func (s *Service) SendMessage(ctx context.Context, errandID uuid.UUID, req SendMessageRequest) (*MessageResponse, error) {
	senderID, err := pkgtypes.UserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	conv, err := s.store.GetOrCreateConversation(ctx, errandID)
	if err != nil {
		return nil, err
	}

	msg, err := s.store.SaveMessage(ctx, conv.ID, senderID, req)
	if err != nil {
		return nil, err
	}

	// Broadcast to any connected WebSocket peers in this conversation.
	resp := toMsgResponse(msg)
	// In a real implementation we'd marshal resp to JSON and call s.hub.Broadcast.

	return &resp, nil
}

func toMsgResponse(m *pkgtypes.Message) MessageResponse {
	return MessageResponse{
		ID:             m.ID.String(),
		ConversationID: m.ConversationID.String(),
		SenderID:       m.SenderID.String(),
		MessageType:    string(m.MessageType),
		Content:        m.Content,
		AttachmentURL:  m.AttachmentURL,
		CreatedAt:      m.CreatedAt,
		ReadAt:         m.ReadAt,
	}
}

// ---- Handler ---------------------------------------------------------------

type Handler struct {
	svc *Service
	log *zap.Logger
}

func NewHandler(svc *Service, log *zap.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// GetConversation godoc
// GET /api/v1/errands/:errandId/chat
func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	conv, err := h.svc.GetConversation(r.Context(), errandID)
	if err != nil {
		if errors.Is(err, ErrConvNotFound) {
			// Return empty conversation rather than 404 — it will be created on first message.
			response.JSON(w, http.StatusOK, ConversationResponse{ErrandID: errandID.String(), Messages: []MessageResponse{}})
			return
		}
		h.log.Error("GetConversation failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSON(w, http.StatusOK, conv)
}

// SendMessage godoc
// POST /api/v1/errands/:errandId/chat/messages
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	errandID, err := uuid.Parse(chi.URLParam(r, "errandId"))
	if err != nil {
		response.BadRequest(w, "INVALID_ID", "errand ID must be a valid UUID")
		return
	}

	var req SendMessageRequest
	if ok, msg := validator.Decode(r, &req); !ok {
		response.BadRequest(w, "VALIDATION_ERROR", msg)
		return
	}

	msg, err := h.svc.SendMessage(r.Context(), errandID, req)
	if err != nil {
		h.log.Error("SendMessage failed", zap.Error(err))
		response.InternalError(w)
		return
	}

	response.JSON(w, http.StatusCreated, msg)
}

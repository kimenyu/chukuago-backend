// Package types contains shared domain structs, enums, and store interfaces
// used across all internal packages. No business logic lives here.
package types

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ── Enums ────────────────────────────────────────────────────────────────────

type UserRole string

const (
	UserRoleClient UserRole = "client"
	UserRoleRunner UserRole = "runner"
	UserRoleAdmin  UserRole = "admin"
)

type UserStatus string

const (
	UserStatusPending   UserStatus = "pending"
	UserStatusActive    UserStatus = "active"
	UserStatusSuspended UserStatus = "suspended"
	UserStatusDeleted   UserStatus = "deleted"
)

type KYCStatus string

const (
	KYCUnsubmitted KYCStatus = "unsubmitted"
	KYCPending     KYCStatus = "pending"
	KYCApproved    KYCStatus = "approved"
	KYCRejected    KYCStatus = "rejected"
)

type ErrandStatus string

const (
	ErrandDraft      ErrandStatus = "draft"
	ErrandPosted     ErrandStatus = "posted"
	ErrandBidding    ErrandStatus = "bidding"
	ErrandAssigned   ErrandStatus = "assigned"
	ErrandInProgress ErrandStatus = "in_progress"
	ErrandDelivered  ErrandStatus = "delivered"
	ErrandCompleted  ErrandStatus = "completed"
	ErrandCancelled  ErrandStatus = "cancelled"
	ErrandDisputed   ErrandStatus = "disputed"
	ErrandExpired    ErrandStatus = "expired"
)

type OfferStatus string

const (
	OfferPending   OfferStatus = "pending"
	OfferAccepted  OfferStatus = "accepted"
	OfferRejected  OfferStatus = "rejected"
	OfferWithdrawn OfferStatus = "withdrawn"
	OfferExpired   OfferStatus = "expired"
)

type StopType string

const (
	StopPickup  StopType = "pickup"
	StopDropoff StopType = "dropoff"
	StopMiddle  StopType = "stop"
)

type ProofType string

const (
	ProofPhoto     ProofType = "photo"
	ProofSignature ProofType = "signature"
	ProofOTP       ProofType = "otp"
	ProofNote      ProofType = "note"
)

type MessageType string

const (
	MessageText   MessageType = "text"
	MessageImage  MessageType = "image"
	MessageFile   MessageType = "file"
	MessageSystem MessageType = "system"
)

type DisputeStatus string

const (
	DisputeOpen        DisputeStatus = "open"
	DisputeUnderReview DisputeStatus = "under_review"
	DisputeResolved    DisputeStatus = "resolved"
	DisputeRejected    DisputeStatus = "rejected"
)

type DisputeResolution string

const (
	ResolutionRefundClient DisputeResolution = "refund_client"
	ResolutionPayRunner    DisputeResolution = "pay_runner"
	ResolutionSplit        DisputeResolution = "split"
	ResolutionNoAction     DisputeResolution = "no_action"
)

type NotificationStatus string

const (
	NotifUnread NotificationStatus = "unread"
	NotifRead   NotificationStatus = "read"
)

// ── Domain structs ───────────────────────────────────────────────────────────

type User struct {
	ID     uuid.UUID  `json:"id"`
	Phone  string     `json:"phone"`
	Email  string     `json:"email,omitempty"`
	Name   string     `json:"name"`
	Role   UserRole   `json:"role"`
	Status UserStatus `json:"status"`

	Password string `json:"-"`

	LastLat    float64   `json:"lastLat,omitempty"`
	LastLng    float64   `json:"lastLng,omitempty"`
	LastSeenAt time.Time `json:"lastSeenAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type UserSession struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"userId"`
	RefreshToken string     `json:"-"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type ClientProfile struct {
	UserID            uuid.UUID  `json:"userId"`
	Bio               string     `json:"bio,omitempty"`
	ProfilePic        string     `json:"profilePic,omitempty"`
	PreferredCurrency string     `json:"preferredCurrency"`
	DefaultRegionID   *uuid.UUID `json:"defaultRegionId,omitempty"`
	TotalErrands      int        `json:"totalErrands"`
	ActiveErrands     int        `json:"activeErrands"`
	DisputeCount      int        `json:"disputeCount"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type RunnerProfile struct {
	UserID         uuid.UUID `json:"userId"`
	Bio            *string   `json:"bio,omitempty"`
	KYCStatus      KYCStatus `json:"kycStatus"`
	VehicleType    *string   `json:"vehicleType,omitempty"`
	VehiclePlate   *string   `json:"vehiclePlate,omitempty"`
	IsAvailable    bool      `json:"isAvailable"`
	RatingAvg      float64   `json:"ratingAvg"`
	RatingCount    int       `json:"ratingCount"`
	CompletedCount int       `json:"completedCount"`
	CancelledCount int       `json:"cancelledCount"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type RunnerKYCDocument struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"userId"`
	DocType    string     `json:"docType"`
	DocURL     string     `json:"docUrl"`
	Status     string     `json:"status"`
	Notes      *string    `json:"notes,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
	ReviewedBy *uuid.UUID `json:"reviewedBy,omitempty"`
}

type RunnerServiceArea struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"userId"`
	RegionID  *uuid.UUID `json:"regionId,omitempty"`
	Label     *string    `json:"label,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type ServiceRegion struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Code      *string   `json:"code,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Errand struct {
	ID          uuid.UUID `json:"id"`
	ClientID    uuid.UUID `json:"clientId"`
	Title       string    `json:"title"`
	Description *string   `json:"description,omitempty"`
	Category    string    `json:"category"`
	Currency    string    `json:"currency"`

	AllowBids     bool       `json:"allowBids"`
	InstantAccept bool       `json:"instantAccept"`
	BiddingEndsAt *time.Time `json:"biddingEndsAt,omitempty"`
	BudgetMin     *float64   `json:"budgetMin,omitempty"`
	BudgetMax     *float64   `json:"budgetMax,omitempty"`
	FixedPrice    *float64   `json:"fixedPrice,omitempty"`

	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`

	Status           ErrandStatus `json:"status"`
	AssignedRunnerID *uuid.UUID   `json:"assignedRunnerId,omitempty"`
	AcceptedOfferID  *uuid.UUID   `json:"acceptedOfferId,omitempty"`
	RegionID         *uuid.UUID   `json:"regionId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ErrandStop struct {
	ID       uuid.UUID `json:"id"`
	ErrandID uuid.UUID `json:"errandId"`
	Seq      int       `json:"seq"`
	StopType StopType  `json:"stopType"`

	AddressLabel *string `json:"addressLabel,omitempty"`
	AddressText  *string `json:"addressText,omitempty"`
	ContactName  *string `json:"contactName,omitempty"`
	ContactPhone *string `json:"contactPhone,omitempty"`

	Lat *float64 `json:"lat,omitempty"`
	Lng *float64 `json:"lng,omitempty"`

	Instructions *string   `json:"instructions,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type ErrandOffer struct {
	ID         uuid.UUID   `json:"id"`
	ErrandID   uuid.UUID   `json:"errandId"`
	RunnerID   uuid.UUID   `json:"runnerId"`
	Amount     float64     `json:"amount"`
	Currency   string      `json:"currency"`
	ETAMinutes *int        `json:"etaMinutes,omitempty"`
	Message    *string     `json:"message,omitempty"`
	Status     OfferStatus `json:"status"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

type ErrandEvent struct {
	ID          uuid.UUID      `json:"id"`
	ErrandID    uuid.UUID      `json:"errandId"`
	ActorUserID *uuid.UUID     `json:"actorUserId,omitempty"`
	EventType   string         `json:"eventType"`
	Notes       *string        `json:"notes,omitempty"`
	Metadata    map[string]any `json:"metadata"`
	OccurredAt  time.Time      `json:"occurredAt"`
}

type DeliveryProof struct {
	ID        uuid.UUID  `json:"id"`
	ErrandID  uuid.UUID  `json:"errandId"`
	StopID    *uuid.UUID `json:"stopId,omitempty"`
	RunnerID  *uuid.UUID `json:"runnerId,omitempty"`
	ProofType ProofType  `json:"proofType"`
	ProofURL  *string    `json:"proofUrl,omitempty"`
	OTPHash   *string    `json:"-"` // never serialised
	Notes     *string    `json:"notes,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type Conversation struct {
	ID        uuid.UUID `json:"id"`
	ErrandID  uuid.UUID `json:"errandId"`
	CreatedAt time.Time `json:"createdAt"`
}

type Message struct {
	ID             uuid.UUID      `json:"id"`
	ConversationID uuid.UUID      `json:"conversationId"`
	SenderID       uuid.UUID      `json:"senderId"`
	MessageType    MessageType    `json:"messageType"`
	Content        *string        `json:"content,omitempty"`
	AttachmentURL  *string        `json:"attachmentUrl,omitempty"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"createdAt"`
	ReadAt         *time.Time     `json:"readAt,omitempty"`
}

type Review struct {
	ID         uuid.UUID `json:"id"`
	ErrandID   uuid.UUID `json:"errandId"`
	ReviewerID uuid.UUID `json:"reviewerId"`
	RevieweeID uuid.UUID `json:"revieweeId"`
	Rating     int       `json:"rating"`
	Comment    *string   `json:"comment,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Dispute struct {
	ID              uuid.UUID          `json:"id"`
	ErrandID        uuid.UUID          `json:"errandId"`
	OpenedByUserID  uuid.UUID          `json:"openedByUserId"`
	Reason          string             `json:"reason"`
	Status          DisputeStatus      `json:"status"`
	Resolution      *DisputeResolution `json:"resolution,omitempty"`
	ResolutionNotes *string            `json:"resolutionNotes,omitempty"`
	AmountClient    *float64           `json:"amountClient,omitempty"`
	AmountRunner    *float64           `json:"amountRunner,omitempty"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
	ResolvedAt      *time.Time         `json:"resolvedAt,omitempty"`
	ResolvedBy      *uuid.UUID         `json:"resolvedBy,omitempty"`
}

type Notification struct {
	ID        uuid.UUID          `json:"id"`
	UserID    uuid.UUID          `json:"userId"`
	Title     string             `json:"title"`
	Body      string             `json:"body"`
	Data      map[string]any     `json:"data"`
	Status    NotificationStatus `json:"status"`
	CreatedAt time.Time          `json:"createdAt"`
	ReadAt    *time.Time         `json:"readAt,omitempty"`
}

type AuditLog struct {
	ID         uuid.UUID      `json:"id"`
	ActorID    *uuid.UUID     `json:"actorId,omitempty"`
	Action     string         `json:"action"`
	EntityType string         `json:"entityType"`
	EntityID   *uuid.UUID     `json:"entityId,omitempty"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// ── Store interfaces (for dependency inversion in tests) ─────────────────────

// UserReader is the minimal read interface other packages depend on.
type UserReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
}

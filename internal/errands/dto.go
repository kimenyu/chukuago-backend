package errands

import "time"

// CreateErrandRequest is the payload for posting a new errand.
type CreateErrandRequest struct {
	Title       string   `json:"title"       validate:"required,min=5,max=150"`
	Description *string  `json:"description" validate:"omitempty,max=2000"`
	Category    string   `json:"category"    validate:"required,oneof=groceries delivery bill_payment queue_standing document errand other"`
	Currency    string   `json:"currency"    validate:"required,len=3"`
	RegionID    *string  `json:"regionId"    validate:"omitempty,uuid"`

	AllowBids     bool     `json:"allowBids"`
	InstantAccept bool     `json:"instantAccept"`
	BudgetMin     *float64 `json:"budgetMin"   validate:"omitempty,gt=0"`
	BudgetMax     *float64 `json:"budgetMax"   validate:"omitempty,gt=0"`
	FixedPrice    *float64 `json:"fixedPrice"  validate:"omitempty,gt=0"`

	ScheduledAt *time.Time `json:"scheduledAt"`
	ExpiresAt   *time.Time `json:"expiresAt"`

	Stops []CreateStopRequest `json:"stops" validate:"required,min=1,dive"`
}

// CreateStopRequest describes one stop in a multi-stop errand.
type CreateStopRequest struct {
	StopType     string   `json:"stopType"     validate:"required,oneof=pickup dropoff stop"`
	AddressLabel *string  `json:"addressLabel" validate:"omitempty,max=200"`
	AddressText  *string  `json:"addressText"  validate:"omitempty,max=500"`
	ContactName  *string  `json:"contactName"  validate:"omitempty,max=100"`
	ContactPhone *string  `json:"contactPhone" validate:"omitempty,e164"`
	Lat          *float64 `json:"lat"          validate:"omitempty,min=-90,max=90"`
	Lng          *float64 `json:"lng"          validate:"omitempty,min=-180,max=180"`
	Instructions *string  `json:"instructions" validate:"omitempty,max=1000"`
}

// UpdateErrandRequest supports partial edits before assignment.
type UpdateErrandRequest struct {
	Title       *string    `json:"title"       validate:"omitempty,min=5,max=150"`
	Description *string    `json:"description" validate:"omitempty,max=2000"`
	BudgetMin   *float64   `json:"budgetMin"   validate:"omitempty,gt=0"`
	BudgetMax   *float64   `json:"budgetMax"   validate:"omitempty,gt=0"`
	FixedPrice  *float64   `json:"fixedPrice"  validate:"omitempty,gt=0"`
	ScheduledAt *time.Time `json:"scheduledAt"`
	ExpiresAt   *time.Time `json:"expiresAt"`
}

// UpdateStatusRequest is used by a runner to advance the errand lifecycle.
type UpdateStatusRequest struct {
	Status string  `json:"status" validate:"required,oneof=in_progress delivered"`
	Notes  *string `json:"notes"  validate:"omitempty,max=500"`
}

// ErrandFeedRequest is the runner's query for available errands.
type ErrandFeedRequest struct {
	Lat      *float64 `json:"lat"      validate:"omitempty,min=-90,max=90"`
	Lng      *float64 `json:"lng"      validate:"omitempty,min=-180,max=180"`
	RadiusKM *float64 `json:"radiusKm" validate:"omitempty,min=1,max=100"`
	Category *string  `json:"category" validate:"omitempty"`
	Page     int      `json:"page"     validate:"min=1"`
	Limit    int      `json:"limit"    validate:"min=1,max=50"`
}

// ListErrandsRequest filters the client's own errand list.
type ListErrandsRequest struct {
	Status *string `json:"status"`
	Page   int
	Limit  int
}

// ErrandResponse is the full errand view returned to clients and runners.
type ErrandResponse struct {
	ID          string     `json:"id"`
	ClientID    string     `json:"clientId"`
	Title       string     `json:"title"`
	Description *string    `json:"description,omitempty"`
	Category    string     `json:"category"`
	Currency    string     `json:"currency"`
	Status      string     `json:"status"`
	AllowBids   bool       `json:"allowBids"`
	BudgetMin   *float64   `json:"budgetMin,omitempty"`
	BudgetMax   *float64   `json:"budgetMax,omitempty"`
	FixedPrice  *float64   `json:"fixedPrice,omitempty"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	ClientName       string     `json:"clientName"` 
	RunnerName       string     `json:"runnerName"`
	Stops       []StopDTO  `json:"stops"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// StopDTO is a serialisable errand stop.
type StopDTO struct {
	ID           string   `json:"id"`
	Seq          int      `json:"seq"`
	StopType     string   `json:"stopType"`
	AddressLabel *string  `json:"addressLabel,omitempty"`
	AddressText  *string  `json:"addressText,omitempty"`
	ContactName  *string  `json:"contactName,omitempty"`
	ContactPhone *string  `json:"contactPhone,omitempty"`
	Lat          *float64 `json:"lat,omitempty"`
	Lng          *float64 `json:"lng,omitempty"`
	Instructions *string  `json:"instructions,omitempty"`
}

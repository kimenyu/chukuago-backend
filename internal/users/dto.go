package users

// UpdateProfileRequest is the payload for patching a user's profile.
// All fields are optional — only non-zero values are applied.
type UpdateProfileRequest struct {
	Name           *string `json:"name"           validate:"omitempty,min=2,max=100"`
	Email          *string `json:"email"          validate:"omitempty,email"`
	Bio            *string `json:"bio"            validate:"omitempty,max=500"`
	ProfilePicture *string `json:"profilePicture" validate:"omitempty,url"`
}

// UpdateLocationRequest carries a user's last-known coordinates.
type UpdateLocationRequest struct {
	Lat float64 `json:"lat" validate:"required,min=-90,max=90"`
	Lng float64 `json:"lng" validate:"required,min=-180,max=180"`
}

// ProfileResponse is the combined view of a user + their client/runner profile.
type ProfileResponse struct {
	ID    string `json:"id"`
	Phone string `json:"phone"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name"`
	Role  string `json:"role"`

	// Only populated for clients
	ClientProfile *ClientProfileDTO `json:"clientProfile,omitempty"`

	// Only populated for runners
	RunnerProfile *RunnerProfileDTO `json:"runnerProfile,omitempty"`
}

type ClientProfileDTO struct {
	Bio               string `json:"bio,omitempty"`
	ProfilePic        string `json:"profilePic,omitempty"`
	PreferredCurrency string `json:"preferredCurrency"`
	TotalErrands      int    `json:"totalErrands"`
	ActiveErrands     int    `json:"activeErrands"`
}

type RunnerProfileDTO struct {
	Bio            *string `json:"bio,omitempty"`
	KYCStatus      string  `json:"kycStatus"`
	VehicleType    *string `json:"vehicleType,omitempty"`
	IsAvailable    bool    `json:"isAvailable"`
	RatingAvg      float64 `json:"ratingAvg"`
	RatingCount    int     `json:"ratingCount"`
	CompletedCount int     `json:"completedCount"`
}

package runners

// SubmitKYCRequest carries the KYC document URLs uploaded to S3 by the mobile client.
// The mobile app uploads directly to cloudinary for now (presigned URL) then sends us the resulting URLs. You can uncomment s3 code and use it 
type SubmitKYCRequest struct {
	NationalIDFrontURL string `json:"nationalIdFrontUrl" validate:"required,url"`
	NationalIDBackURL  string `json:"nationalIdBackUrl"  validate:"required,url"`
	SelfieURL          string `json:"selfieUrl"          validate:"required,url"`
}

// SetAvailabilityRequest toggles a runner's online/offline state.
type SetAvailabilityRequest struct {
	IsAvailable bool `json:"isAvailable"`
}

// AddServiceAreaRequest attaches a region or custom zone to the runner's coverage.
type AddServiceAreaRequest struct {
	RegionID *string `json:"regionId" validate:"omitempty,uuid"`
	Label    *string `json:"label"    validate:"omitempty,max=100"`
}

// UpdateRunnerProfileRequest allows a runner to update their bio and vehicle info.
type UpdateRunnerProfileRequest struct {
	Bio          *string `json:"bio"          validate:"omitempty,max=500"`
	VehicleType  *string `json:"vehicleType"  validate:"omitempty,oneof=motorbike bicycle car van foot"`
	VehiclePlate *string `json:"vehiclePlate" validate:"omitempty,max=20"`
}

// KYCStatusResponse is the lightweight response for a runner's KYC check.
type KYCStatusResponse struct {
	Status string  `json:"status"`
	Notes  *string `json:"notes,omitempty"`
}

// ServiceAreaResponse represents one of a runner's declared service zones.
type ServiceAreaResponse struct {
	ID         string  `json:"id"`
	RegionID   *string `json:"regionId,omitempty"`
	RegionName *string `json:"regionName,omitempty"`
	Label      *string `json:"label,omitempty"`
}

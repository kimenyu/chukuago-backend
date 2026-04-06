package auth

// SendOTPRequest is the payload for initiating phone OTP verification.
type SendOTPRequest struct {
	Phone string `json:"phone" validate:"required,e164"`
	Role  string `json:"role"  validate:"required,oneof=client runner"`
}

// VerifyOTPRequest is the payload for completing OTP verification and signing in.
type VerifyOTPRequest struct {
	Phone string `json:"phone" validate:"required,e164"`
	OTP   string `json:"otp"   validate:"required,len=6"`
}

// RefreshRequest is the payload for obtaining a new access token.
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken" validate:"required"`
}

// TokenPair is the response returned after successful authentication.
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

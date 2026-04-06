package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	atLiveURL    = "https://api.africastalking.com/version1/messaging"
	atSandboxURL = "https://api.sandbox.africastalking.com/version1/messaging"
)

// AfricasTalkingProvider implements SMSProvider via the Africa's Talking gateway.
// It is the recommended provider for ChukuaGo because:
//   - Competitive rates for Kenyan networks (Safaricom, Airtel, Telkom)
//   - Supports Safaricom short codes for branded sender IDs
//   - Phase 2 M-Pesa STK push is also available through the same SDK
type AfricasTalkingProvider struct {
	apiKey   string
	username string
	senderID string
	sandbox  bool
	client   *http.Client
}

// NewAfricasTalkingProvider creates a ready-to-use Africa's Talking SMS provider.
// Set sandbox=true when APP_ENV != production to route requests to the AT sandbox.
func NewAfricasTalkingProvider(apiKey, username, senderID string, sandbox bool) *AfricasTalkingProvider {
	return &AfricasTalkingProvider{
		apiKey:   apiKey,
		username: username,
		senderID: senderID,
		sandbox:  sandbox,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *AfricasTalkingProvider) baseURL() string {
	if p.sandbox {
		return atSandboxURL
	}
	return atLiveURL
}

// SendSMS dispatches an SMS via the Africa's Talking REST API.
func (p *AfricasTalkingProvider) SendSMS(ctx context.Context, phone, message string) error {
	form := url.Values{}
	form.Set("username", p.username)
	form.Set("to", phone)
	form.Set("message", message)
	if p.senderID != "" && !p.sandbox {
		// Sandbox ignores the sender ID — omitting it avoids confusing error responses.
		form.Set("from", p.senderID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build AT request: %w", err)
	}
	req.Header.Set("apiKey", p.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send AT request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AT API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

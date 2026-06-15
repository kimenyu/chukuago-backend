// Package storage handles file uploads for KYC documents and chat images.

package storage

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cloudinaryUploadURL = "https://api.cloudinary.com/v1_1/%s/auto/upload"

// AllowedMIMETypes restricts uploads to safe content types.
// Detection is done on file bytes, not the filename extension.
var AllowedMIMETypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"application/pdf": true,
}

// MaxUploadSize is 10 MB.
const MaxUploadSize = 10 << 20

// UploadResult is returned after a successful upload.
type UploadResult struct {
	URL         string
	PublicID    string
	Format      string
	ContentType string
	SizeBytes   int64
}

// CloudinaryClient uploads files to Cloudinary's REST API using signed requests.
type CloudinaryClient struct {
	cloudName string
	apiKey    string
	apiSecret string
	httpClient *http.Client
}

// NewCloudinaryClient creates a ready-to-use Cloudinary uploader.
// Credentials come from environment variables — never hard-code them.
func NewCloudinaryClient(cloudName, apiKey, apiSecret string) *CloudinaryClient {
	return &CloudinaryClient{
		cloudName: cloudName,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Upload validates a multipart file and uploads it to Cloudinary.
// The folder parameter maps to a Cloudinary folder (e.g. "kyc", "chat").
func (c *CloudinaryClient) Upload(ctx context.Context, folder string, fh *multipart.FileHeader) (*UploadResult, error) {
	if fh.Size > MaxUploadSize {
		return nil, fmt.Errorf("file exceeds maximum size of %d MB", MaxUploadSize>>20)
	}

	file, err := fh.Open()
	if err != nil {
		return nil, fmt.Errorf("open uploaded file: %w", err)
	}
	defer file.Close()

	// Detect MIME type from the first 512 bytes — not from the filename.
	header := make([]byte, 512)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("read file header: %w", err)
	}
	contentType := http.DetectContentType(header[:n])
	if !AllowedMIMETypes[contentType] {
		return nil, fmt.Errorf("file type %q is not permitted", contentType)
	}

	// Seek back so the full file body is uploaded.
	if seeker, ok := file.(io.Seeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek file: %w", err)
		}
	}

	// Read entire file for the multipart body.
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	params := map[string]string{
		"folder":    folder,
		"timestamp": timestamp,
	}
	signature := c.sign(params)

	// Build the multipart form.
	body := &strings.Builder{}
	boundary := "----ChukuaGoBoundary"
	writeField := func(name, value string) {
		fmt.Fprintf(body, "--%s\r\nContent-Disposition: form-data; name=%q\r\n\r\n%s\r\n", boundary, name, value)
	}

	writeField("api_key", c.apiKey)
	writeField("timestamp", timestamp)
	writeField("folder", folder)
	writeField("signature", signature)

	// File field.
	fmt.Fprintf(body, "--%s\r\nContent-Disposition: form-data; name=\"file\"; filename=%q\r\nContent-Type: %s\r\n\r\n",
		boundary, fh.Filename, contentType)
	body.Write(data) //nolint:errcheck
	fmt.Fprintf(body, "\r\n--%s--\r\n", boundary)

	uploadURL := fmt.Sprintf(cloudinaryUploadURL, c.cloudName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL,
		strings.NewReader(body.String()))
	if err != nil {
		return nil, fmt.Errorf("build cloudinary request: %w", err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudinary upload request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		SecureURL string `json:"secure_url"`
		PublicID  string `json:"public_id"`
		Format    string `json:"format"`
		Bytes     int64  `json:"bytes"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode cloudinary response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("cloudinary error: %s", result.Error.Message)
	}

	return &UploadResult{
		URL:         result.SecureURL,
		PublicID:    result.PublicID,
		Format:      result.Format,
		ContentType: contentType,
		SizeBytes:   result.Bytes,
	}, nil
}

// UploadFromURL uploads a file that is already accessible via an HTTPS URL.
// Useful for re-processing existing assets.
func (c *CloudinaryClient) UploadFromURL(ctx context.Context, folder, fileURL string) (*UploadResult, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	params := map[string]string{
		"file":      fileURL,
		"folder":    folder,
		"timestamp": timestamp,
	}
	signature := c.sign(params)

	form := url.Values{}
	form.Set("api_key", c.apiKey)
	form.Set("timestamp", timestamp)
	form.Set("folder", folder)
	form.Set("signature", signature)
	form.Set("file", fileURL)

	uploadURL := fmt.Sprintf(cloudinaryUploadURL, c.cloudName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudinary request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		SecureURL string `json:"secure_url"`
		PublicID  string `json:"public_id"`
		Format    string `json:"format"`
		Bytes     int64  `json:"bytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &UploadResult{
		URL:      result.SecureURL,
		PublicID: result.PublicID,
		Format:   result.Format,
	}, nil
}

// sign builds the SHA-1 HMAC signature Cloudinary requires for authenticated uploads.
// See: https://cloudinary.com/documentation/authentication#generating_signatures
func (c *CloudinaryClient) sign(params map[string]string) string {
	// Sort keys for a deterministic signing string.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(params))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}

	payload := strings.Join(parts, "&") + c.apiSecret
	h := sha1.New()
	h.Write([]byte(payload)) //nolint:errcheck
	return fmt.Sprintf("%x", h.Sum(nil))
}

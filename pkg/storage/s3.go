package storage

//
//import (
//	"context"
//	"fmt"
//	"io"
//	"mime/multipart"
//	"net/http"
//	"path/filepath"
//	"strings"
//	"time"
//
//	"github.com/google/uuid"
//)
//
//// AllowedMIMETypes restricts file uploads to safe content types.
//var AllowedMIMETypes = map[string]string{
//	"image/jpeg": ".jpg",
//	"image/png":  ".png",
//	"image/webp": ".webp",
//	"application/pdf": ".pdf",
//}
//
//// MaxUploadSize is 10 MB — sufficient for KYC documents and chat images.
//const MaxUploadSize = 10 << 20
//
//// S3Client is an abstraction over the AWS SDK so the rest of the codebase
//// doesn't import the SDK directly. In tests, swap in a mock.
//type S3Client interface {
//	PutObject(ctx context.Context, bucket, key, contentType string, body io.Reader) (string, error)
//}
//
//// Uploader validates and stores file uploads to S3.
//type Uploader struct {
//	client S3Client
//	bucket string
//	region string
//}
//
//// NewUploader creates an Uploader backed by the given S3 client.
//func NewUploader(client S3Client, bucket, region string) *Uploader {
//	return &Uploader{client: client, bucket: bucket, region: region}
//}
//
//// UploadResult carries the S3 object URL and metadata after a successful upload.
//type UploadResult struct {
//	URL         string
//	ContentType string
//	SizeBytes   int64
//}
//
//// Upload validates and stores a multipart file upload.
//// Returns the public HTTPS URL of the stored object.
//func (u *Uploader) Upload(ctx context.Context, folder string, fh *multipart.FileHeader) (*UploadResult, error) {
//	if fh.Size > MaxUploadSize {
//		return nil, fmt.Errorf("file exceeds maximum size of %d MB", MaxUploadSize>>20)
//	}
//
//	file, err := fh.Open()
//	if err != nil {
//		return nil, fmt.Errorf("open uploaded file: %w", err)
//	}
//	defer file.Close()
//
//	// Detect MIME type from the first 512 bytes (not from the filename extension).
//	buf := make([]byte, 512)
//	n, err := file.Read(buf)
//	if err != nil && err != io.EOF {
//		return nil, fmt.Errorf("read file header: %w", err)
//	}
//	contentType := http.DetectContentType(buf[:n])
//
//	ext, ok := AllowedMIMETypes[contentType]
//	if !ok {
//		return nil, fmt.Errorf("file type %q is not allowed", contentType)
//	}
//
//	// Seek back so the full file is uploaded, not just the remainder after detection.
//	if seeker, ok := file.(io.Seeker); ok {
//		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
//			return nil, fmt.Errorf("seek file: %w", err)
//		}
//	}
//
//	// Build a collision-proof storage key: uploads/{folder}/{date}/{uuid}{ext}
//	key := fmt.Sprintf("uploads/%s/%s/%s%s",
//		folder,
//		time.Now().Format("2006/01/02"),
//		uuid.New().String(),
//		ext,
//	)
//	// Sanitise the folder to prevent path traversal.
//	key = filepath.Clean(key)
//	key = strings.TrimPrefix(key, "/")
//
//	publicURL, err := u.client.PutObject(ctx, u.bucket, key, contentType, file)
//	if err != nil {
//		return nil, fmt.Errorf("upload to S3: %w", err)
//	}
//
//	return &UploadResult{
//		URL:         publicURL,
//		ContentType: contentType,
//		SizeBytes:   fh.Size,
//	}, nil
//}
//
//// AWSS3Client implements S3Client using pre-signed PutObject calls.
//// In production wire this up with github.com/aws/aws-sdk-go-v2/service/s3.
//type AWSS3Client struct {
//	region string
//	bucket string
//	// In a real implementation: s3Client *s3.Client
//}
//
//// NewAWSS3Client creates an AWSS3Client.
//// Replace the stub body below with the real AWS SDK v2 client initialisation.
//func NewAWSS3Client(region, bucket, accessKey, secretKey string) *AWSS3Client {
//	return &AWSS3Client{region: region, bucket: bucket}
//}
//
//// PutObject uploads the body to S3 and returns the HTTPS URL.
//func (c *AWSS3Client) PutObject(ctx context.Context, bucket, key, contentType string, body io.Reader) (string, error) {
//	// Wire in aws-sdk-go-v2 here:
//	//
//	//   output, err := c.s3Client.PutObject(ctx, &s3.PutObjectInput{
//	//       Bucket:      aws.String(bucket),
//	//       Key:         aws.String(key),
//	//       Body:        body,
//	//       ContentType: aws.String(contentType),
//	//       ACL:         types.ObjectCannedACLPrivate,
//	//   })
//	//
//	// For now return a predictable URL so callers can integrate without the SDK.
//	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, c.region, key)
//	return url, nil
//}

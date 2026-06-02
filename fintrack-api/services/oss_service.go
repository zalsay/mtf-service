package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"fintrack-api/config"

	alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type OSSService struct {
	config *config.OSSConfig
	client *alioss.Client
}

func NewOSSService(cfg *config.Config) *OSSService {
	service := &OSSService{}
	if cfg == nil {
		return service
	}

	service.config = &cfg.OSS
	if !service.Enabled() {
		return service
	}

	ossCfg := alioss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(service.config.AccessKeyID, service.config.AccessKeySecret)).
		WithRegion(service.config.Region).
		WithEndpoint(service.config.Endpoint).
		WithDisableSSL(service.config.DisableSSL).
		WithUsePathStyle(service.config.UsePathStyle).
		WithConnectTimeout(time.Duration(service.config.ConnectTimeout) * time.Second).
		WithReadWriteTimeout(time.Duration(service.config.ReadWriteTimeout) * time.Second).
		WithInsecureSkipVerify(service.config.InsecureSkipVerify)

	service.client = alioss.NewClient(ossCfg)
	return service
}

func (s *OSSService) Enabled() bool {
	return s != nil &&
		s.config != nil &&
		s.config.Enabled &&
		strings.TrimSpace(s.config.Endpoint) != "" &&
		strings.TrimSpace(s.config.Region) != "" &&
		strings.TrimSpace(s.config.Bucket) != "" &&
		strings.TrimSpace(s.config.AccessKeyID) != "" &&
		strings.TrimSpace(s.config.AccessKeySecret) != ""
}

func (s *OSSService) BuildObjectKey(userID int, relativePath string) (string, error) {
	cleanedPath, err := normalizeUZIReportPath(relativePath)
	if err != nil {
		return "", err
	}

	prefix := strings.Trim(path.Clean("/"+firstNonEmpty(strings.TrimSpace(s.config.Prefix), "uzi-reports")), "/")
	if prefix == "." {
		prefix = "uzi-reports"
	}
	return path.Join(prefix, strconv.Itoa(userID), cleanedPath), nil
}

func (s *OSSService) UploadHTML(ctx context.Context, userID int, relativePath string, body []byte, contentType string) (string, int64, error) {
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}
	return s.UploadObject(ctx, userID, relativePath, body, contentType, "private, no-store, max-age=0")
}

func (s *OSSService) UploadObject(ctx context.Context, userID int, relativePath string, body []byte, contentType string, cacheControl string) (string, int64, error) {
	if !s.Enabled() || s.client == nil {
		return "", 0, errors.New("oss service is not enabled")
	}

	objectKey, err := s.BuildObjectKey(userID, relativePath)
	if err != nil {
		return "", 0, err
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if cacheControl == "" {
		cacheControl = "private, max-age=86400"
	}

	contentLength := int64(len(body))
	_, err = s.client.PutObject(ctx, &alioss.PutObjectRequest{
		Bucket:             alioss.Ptr(strings.TrimSpace(s.config.Bucket)),
		Key:                alioss.Ptr(objectKey),
		Body:               bytes.NewReader(body),
		ContentType:        alioss.Ptr(contentType),
		ContentDisposition: alioss.Ptr("inline"),
		ContentLength:      alioss.Ptr(contentLength),
		CacheControl:       alioss.Ptr(cacheControl),
	})
	if err != nil {
		return "", 0, fmt.Errorf("put oss object: %w", err)
	}

	return objectKey, contentLength, nil
}

func (s *OSSService) GetObject(ctx context.Context, objectKey string) ([]byte, string, error) {
	if !s.Enabled() || s.client == nil {
		return nil, "", errors.New("oss service is not enabled")
	}
	if strings.TrimSpace(objectKey) == "" {
		return nil, "", errors.New("oss object key is required")
	}

	result, err := s.client.GetObject(ctx, &alioss.GetObjectRequest{
		Bucket: alioss.Ptr(strings.TrimSpace(s.config.Bucket)),
		Key:    alioss.Ptr(strings.TrimSpace(objectKey)),
	})
	if err != nil {
		return nil, "", fmt.Errorf("get oss object: %w", err)
	}
	defer result.Body.Close()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read oss object: %w", err)
	}

	contentType := ""
	if result.ContentType != nil {
		contentType = strings.TrimSpace(*result.ContentType)
	}
	return body, contentType, nil
}

func (s *OSSService) DeleteObject(ctx context.Context, objectKey string) error {
	if !s.Enabled() || s.client == nil {
		return nil
	}
	if strings.TrimSpace(objectKey) == "" {
		return nil
	}

	_, err := s.client.DeleteObject(ctx, &alioss.DeleteObjectRequest{
		Bucket: alioss.Ptr(strings.TrimSpace(s.config.Bucket)),
		Key:    alioss.Ptr(strings.TrimSpace(objectKey)),
	})
	if err != nil {
		return fmt.Errorf("delete oss object: %w", err)
	}
	return nil
}

func (s *OSSService) PresignGetObjectURL(ctx context.Context, objectKey string) (string, error) {
	if !s.Enabled() || s.client == nil {
		return "", errors.New("oss service is not enabled")
	}
	if strings.TrimSpace(objectKey) == "" {
		return "", errors.New("oss object key is required")
	}

	expires := time.Duration(s.config.SignedURLTTL) * time.Second
	if expires <= 0 {
		expires = 5 * time.Minute
	}

	result, err := s.client.Presign(ctx, &alioss.GetObjectRequest{
		Bucket:                     alioss.Ptr(strings.TrimSpace(s.config.Bucket)),
		Key:                        alioss.Ptr(strings.TrimSpace(objectKey)),
		ResponseContentDisposition: alioss.Ptr("inline"),
	}, alioss.PresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("presign oss object: %w", err)
	}
	return result.URL, nil
}

func (s *OSSService) HasPublicBaseURL() bool {
	return s != nil && s.config != nil && strings.TrimSpace(s.config.PublicBaseURL) != ""
}

func (s *OSSService) RewriteSignedURLToPublicBase(signedURL string) (string, error) {
	if !s.HasPublicBaseURL() {
		return signedURL, nil
	}

	publicBase, err := neturl.Parse(strings.TrimSpace(s.config.PublicBaseURL))
	if err != nil {
		return "", fmt.Errorf("parse oss public base url: %w", err)
	}
	if publicBase.Scheme == "" || publicBase.Host == "" {
		return "", errors.New("oss public base url must include scheme and host")
	}

	parsedSignedURL, err := neturl.Parse(strings.TrimSpace(signedURL))
	if err != nil {
		return "", fmt.Errorf("parse signed url: %w", err)
	}

	parsedSignedURL.Scheme = publicBase.Scheme
	parsedSignedURL.Host = publicBase.Host
	parsedSignedURL.User = publicBase.User

	basePath := strings.TrimRight(publicBase.Path, "/")
	if basePath != "" {
		parsedSignedURL.Path = basePath + "/" + strings.TrimLeft(parsedSignedURL.Path, "/")
	}

	return parsedSignedURL.String(), nil
}

func (s *OSSService) IsManagedObjectKey(value string) bool {
	if !s.Enabled() {
		return false
	}

	prefix := strings.Trim(path.Clean("/"+firstNonEmpty(strings.TrimSpace(s.config.Prefix), "uzi-reports")), "/")
	key := strings.TrimSpace(value)
	return key != "" && (key == prefix || strings.HasPrefix(key, prefix+"/"))
}

func (s *OSSService) SignedURLTTLSeconds() int {
	if s == nil || s.config == nil || s.config.SignedURLTTL <= 0 {
		return 300
	}
	return s.config.SignedURLTTL
}

func isOSSObjectMissing(err error) bool {
	if err == nil {
		return false
	}

	type serviceError interface {
		ErrorCode() string
	}

	var ossErr *alioss.ServiceError
	if errors.As(err, &ossErr) {
		return ossErr.StatusCode == http.StatusNotFound || ossErr.Code == "NoSuchKey"
	}

	var codedErr serviceError
	if errors.As(err, &codedErr) {
		return codedErr.ErrorCode() == "NoSuchKey"
	}

	return strings.Contains(strings.ToLower(err.Error()), "nosuchkey")
}

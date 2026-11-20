package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ispilolite/api/dto"
)

const (
	mediaMaxProfile   = 5 << 20
	mediaMaxProof     = 15 << 20
	mediaMaxPromotion = 10 << 20
	mediaMaxChat      = 10 << 20
	mediaMaxProxyBody = mediaMaxProof + 2<<20
	r2UploadTTL       = 10 * time.Minute
)

var allowedMediaMIME = map[string]string{
	"image/jpeg": ".jpg",
	"image/jpg":  ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/heic": ".heic",
	"image/heif": ".heif",
}

type mediaKind struct {
	Folder   string
	MaxBytes int64
	Roles    map[string]bool
}

var mediaKinds = map[string]mediaKind{
	"profile":            {Folder: "profiles", MaxBytes: mediaMaxProfile, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
	"installation_proof": {Folder: "installation-proofs", MaxBytes: mediaMaxProof, Roles: mediaRoles("technician", "isp", "admin", "both")},
	"job_request":        {Folder: "job-requests", MaxBytes: mediaMaxProof, Roles: mediaRoles("customer", "admin", "both")},
	"portfolio":          {Folder: "technician-portfolio", MaxBytes: mediaMaxProof, Roles: mediaRoles("technician", "admin", "both")},
	"promotion":          {Folder: "promotions", MaxBytes: mediaMaxPromotion, Roles: mediaRoles("isp", "admin", "both")},
	"isp_logo":           {Folder: "isp-logos", MaxBytes: mediaMaxProfile, Roles: mediaRoles("isp", "admin", "both")},
	"billing_logo":       {Folder: "billing-logos", MaxBytes: mediaMaxProfile, Roles: mediaRoles("isp", "admin", "both")},
	"review":             {Folder: "reviews", MaxBytes: mediaMaxProfile, Roles: mediaRoles("customer", "admin", "both")},
	"chat":               {Folder: "chat", MaxBytes: mediaMaxChat, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
	"identity_document":  {Folder: "identity-documents", MaxBytes: mediaMaxProof, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
	"id_photo":           {Folder: "identity-documents", MaxBytes: mediaMaxProof, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
	"document":           {Folder: "documents", MaxBytes: mediaMaxProof, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
}

// MediaHandler issues short-lived Cloudflare R2 presigned PUT URLs. Flutter
// uploads bytes directly to R2, then stores public_url through the relevant
// profile, portfolio, proof, request, or billing endpoint. Access keys never
// leave this service.
type MediaHandler struct {
	endpoint      string
	accessKeyID   string
	secretKey     string
	bucket        string
	publicBaseURL string
	region        string
	folderPrefix  string
	client        *http.Client
	now           func() time.Time
}

func NewMediaHandler() *MediaHandler {
	return newMediaHandler(
		os.Getenv("R2_ENDPOINT"),
		os.Getenv("R2_ACCESS_KEY_ID"),
		os.Getenv("R2_SECRET_ACCESS_KEY"),
		os.Getenv("R2_BUCKET"),
		os.Getenv("R2_PUBLIC_BASE_URL"),
		os.Getenv("R2_REGION"),
		os.Getenv("R2_FOLDER_PREFIX"),
	)
}

func newMediaHandler(endpoint, accessKeyID, secretKey, bucket, publicBaseURL, region, folderPrefix string) *MediaHandler {
	if strings.TrimSpace(region) == "" {
		region = "auto"
	}
	return &MediaHandler{
		endpoint:      strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		accessKeyID:   strings.TrimSpace(accessKeyID),
		secretKey:     strings.TrimSpace(secretKey),
		bucket:        sanitizePathPart(bucket),
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		region:        strings.TrimSpace(region),
		folderPrefix:  sanitizeFolderPath(folderPrefix),
		client:        &http.Client{Timeout: 45 * time.Second},
		now:           time.Now,
	}
}

func (h *MediaHandler) configured() bool {
	if h == nil || h.accessKeyID == "" || h.secretKey == "" || h.bucket == "" {
		return false
	}
	endpoint, endpointErr := url.Parse(h.endpoint)
	publicURL, publicErr := url.Parse(h.publicBaseURL)
	return endpointErr == nil && publicErr == nil && endpoint.Scheme == "https" && endpoint.Host != "" && publicURL.Scheme == "https" && publicURL.Host != ""
}

type mediaUploadRequest struct {
	Kind        string `json:"kind"`
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	ContentType string `json:"content_type"`
}

func (h *MediaHandler) Signature(w http.ResponseWriter, r *http.Request) {
	if h == nil || !h.configured() {
		respondWithErrorRaw(w, http.StatusServiceUnavailable, "R2 media storage is not configured")
		return
	}
	userID := userIDFromContext(r.Context())
	if userID == "" {
		respondWithErrorRaw(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var input mediaUploadRequest
	if err := decodeJSON(w, r, &input); err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	kindName, kind, contentType, err := validateMediaInput(input, userRoleFromContext(r.Context()))
	if err != nil {
		writeMediaInputError(w, err)
		return
	}
	key, err := h.objectKey(kind, userID, input.FileName, contentType)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	uploadURL, expiresAt, err := h.presignedPutURL(key, h.now().UTC(), r2UploadTTL)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{
		"kind": kindName, "method": http.MethodPut, "upload_url": uploadURL,
		"public_url": h.publicURL(key), "object_key": key,
		"expires_at": expiresAt.Unix(), "max_file_size": kind.MaxBytes,
		"headers": map[string]string{"Content-Type": contentType},
	}})
}

// Upload is a same-origin fallback for clients that cannot PUT to R2 directly.
// The primary mobile flow uses Signature and bypasses API bandwidth.
func (h *MediaHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if h == nil || !h.configured() {
		respondWithErrorRaw(w, http.StatusServiceUnavailable, "R2 media storage is not configured")
		return
	}
	userID := userIDFromContext(r.Context())
	if userID == "" {
		respondWithErrorRaw(w, http.StatusUnauthorized, "authentication required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mediaMaxProxyBody)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			respondWithErrorRaw(w, http.StatusRequestEntityTooLarge, "uploaded file is too large")
			return
		}
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid multipart upload")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	input := mediaUploadRequest{Kind: r.FormValue("kind"), FileName: header.Filename, FileSize: header.Size, ContentType: header.Header.Get("Content-Type")}
	if normalizeContentType(input.ContentType) == "application/octet-stream" {
		input.ContentType = mimeFromFilename(input.FileName)
	}
	kindName, kind, requestedType, err := validateMediaInput(input, userRoleFromContext(r.Context()))
	if err != nil {
		writeMediaInputError(w, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, kind.MaxBytes+1))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	if len(data) == 0 || int64(len(data)) > kind.MaxBytes {
		respondWithErrorRaw(w, http.StatusRequestEntityTooLarge, "uploaded file is empty or too large")
		return
	}
	detectedType := normalizeContentType(http.DetectContentType(data))
	if _, ok := allowedMediaMIME[detectedType]; !ok && (requestedType == "image/heic" || requestedType == "image/heif") {
		detectedType = requestedType
	}
	if _, ok := allowedMediaMIME[detectedType]; !ok {
		respondWithErrorRaw(w, http.StatusBadRequest, "file must be a supported image type")
		return
	}
	if requestedType != "" && requestedType != detectedType && !(requestedType == "image/heic" || requestedType == "image/heif") {
		respondWithErrorRaw(w, http.StatusBadRequest, "file content does not match content_type")
		return
	}
	key, err := h.objectKey(kind, userID, header.Filename, detectedType)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	uploadURL, _, err := h.presignedPutURL(key, h.now().UTC(), r2UploadTTL)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	req.Header.Set("Content-Type", detectedType)
	resp, err := h.client.Do(req)
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadGateway, "R2 media upload failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respondWithErrorRaw(w, http.StatusBadGateway, "R2 media upload failed")
		return
	}
	publicURL := h.publicURL(key)
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]any{
		"kind": kindName, "public_url": publicURL, "secure_url": publicURL,
		"object_key": key, "bytes": len(data), "content_type": detectedType,
		"uploaded_via": "server_fallback",
	}})
}

var (
	errUnknownMediaKind = errors.New("unknown media kind")
	errMediaRole        = errors.New("media kind is not allowed for this role")
	errMediaSize        = errors.New("invalid media size")
	errMediaType        = errors.New("unsupported media type")
)

func validateMediaInput(input mediaUploadRequest, role string) (string, mediaKind, string, error) {
	kindName := strings.ToLower(strings.TrimSpace(input.Kind))
	kind, ok := mediaKinds[kindName]
	if !ok {
		return "", mediaKind{}, "", errUnknownMediaKind
	}
	if !kind.Roles[role] {
		return "", mediaKind{}, "", errMediaRole
	}
	if input.FileSize < 0 || input.FileSize > kind.MaxBytes {
		return "", mediaKind{}, "", errMediaSize
	}
	contentType := normalizeContentType(input.ContentType)
	if contentType == "" {
		contentType = mimeFromFilename(input.FileName)
	}
	if _, ok := allowedMediaMIME[contentType]; !ok {
		return "", mediaKind{}, "", errMediaType
	}
	return kindName, kind, contentType, nil
}

func writeMediaInputError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnknownMediaKind), errors.Is(err, errMediaType):
		respondWithErrorRaw(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errMediaRole):
		respondWithErrorRaw(w, http.StatusForbidden, "your account is not allowed to upload this media kind")
	case errors.Is(err, errMediaSize):
		respondWithErrorRaw(w, http.StatusRequestEntityTooLarge, "file exceeds the allowed size")
	default:
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid media upload")
	}
}

func (h *MediaHandler) objectKey(kind mediaKind, userID, filename, contentType string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	ext := allowedMediaMIME[contentType]
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(safeUploadFilename(filename)))
	}
	now := h.now().UTC()
	return strings.Join([]string{h.folderPrefix, kind.Folder, sanitizePathPart(userID), now.Format("2006"), now.Format("01"), hex.EncodeToString(random[:]) + ext}, "/"), nil
}

func (h *MediaHandler) objectURL(key string) (string, error) {
	endpoint, err := url.Parse(h.endpoint)
	if err != nil {
		return "", err
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/" + h.bucket + "/" + key
	return endpoint.String(), nil
}

func (h *MediaHandler) publicURL(key string) string {
	return h.publicBaseURL + "/" + escapeObjectPath(key)
}

func (h *MediaHandler) presignedPutURL(key string, at time.Time, ttl time.Duration) (string, time.Time, error) {
	rawURL, err := h.objectURL(key)
	if err != nil {
		return "", time.Time{}, err
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", time.Time{}, err
	}
	amzDate := at.Format("20060102T150405Z")
	date := at.Format("20060102")
	scope := date + "/" + h.region + "/s3/aws4_request"
	query := url.Values{}
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", h.accessKeyID+"/"+scope)
	query.Set("X-Amz-Date", amzDate)
	query.Set("X-Amz-Expires", strconv.FormatInt(int64(ttl.Seconds()), 10))
	query.Set("X-Amz-SignedHeaders", "host")
	u.RawQuery = query.Encode()
	canonicalRequest := strings.Join([]string{http.MethodPut, u.EscapedPath(), canonicalQuery(u.Query()), "host:" + strings.ToLower(u.Host) + "\n", "host", "UNSIGNED-PAYLOAD"}, "\n")
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, hex.EncodeToString(requestHash[:])}, "\n")
	signingKey := r2SigningKey(h.secretKey, date, h.region)
	signature := hmacSHA256Hex(signingKey, stringToSign)
	query.Set("X-Amz-Signature", signature)
	u.RawQuery = query.Encode()
	return u.String(), at.Add(ttl), nil
}

func r2SigningKey(secret, date, region string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func hmacSHA256Hex(key []byte, value string) string {
	return hex.EncodeToString(hmacSHA256(key, value))
}

func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range values[key] {
			parts = append(parts, awsEscape(key)+"="+awsEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func awsEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func escapeObjectPath(value string) string {
	parts := strings.Split(value, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func safeUploadFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." {
		return "upload"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-.")
	if cleaned == "" {
		return "upload"
	}
	if len(cleaned) > 120 {
		cleaned = cleaned[:120]
	}
	return cleaned
}

func mediaRoles(roles ...string) map[string]bool {
	out := make(map[string]bool, len(roles))
	for _, role := range roles {
		out[role] = true
	}
	return out
}

func normalizeContentType(value string) string {
	if idx := strings.IndexByte(value, ';'); idx >= 0 {
		value = value[:idx]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func mimeFromFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".heic":
		return "image/heic"
	case ".heif":
		return "image/heif"
	default:
		return ""
	}
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".-")
}

func sanitizeFolderPath(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "ispilolite"
	}
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(value, "/") {
		if clean := sanitizePathPart(part); clean != "" {
			parts = append(parts, clean)
		}
	}
	if len(parts) == 0 {
		return "ispilolite"
	}
	return strings.Join(parts, "/")
}

package handlers

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ispilolite/api/dto"
)

const (
	mediaAllowedFormats = "jpg,jpeg,png,webp,heic,heif"
	mediaMaxProfile     = 5 << 20
	mediaMaxProof       = 15 << 20
	mediaMaxPromotion   = 10 << 20
	mediaMaxChat        = 10 << 20
	// The fallback proxy accepts the largest supported file plus multipart
	// overhead. Per-kind limits are still enforced after parsing.
	mediaMaxProxyBody = mediaMaxProof + 2<<20
)

var allowedMediaMIME = map[string]bool{
	"image/jpeg": true,
	"image/jpg":  true,
	"image/png":  true,
	"image/webp": true,
	"image/heic": true,
	"image/heif": true,
}

type mediaKind struct {
	Folder   string
	MaxBytes int64
	Roles    map[string]bool
}

var mediaKinds = map[string]mediaKind{
	"profile":            {Folder: "profiles", MaxBytes: mediaMaxProfile, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
	"installation_proof": {Folder: "installation-proofs", MaxBytes: mediaMaxProof, Roles: mediaRoles("technician", "isp", "admin", "both")},
	"portfolio":          {Folder: "technician-portfolio", MaxBytes: mediaMaxProof, Roles: mediaRoles("technician", "admin", "both")},
	"promotion":          {Folder: "promotions", MaxBytes: mediaMaxPromotion, Roles: mediaRoles("isp", "admin", "both")},
	"isp_logo":           {Folder: "isp-logos", MaxBytes: mediaMaxProfile, Roles: mediaRoles("isp", "admin", "both")},
	"review":             {Folder: "reviews", MaxBytes: mediaMaxProfile, Roles: mediaRoles("customer", "admin", "both")},
	"chat":               {Folder: "chat", MaxBytes: mediaMaxChat, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
	"identity_document":  {Folder: "identity-documents", MaxBytes: mediaMaxProof, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
	"id_photo":           {Folder: "identity-documents", MaxBytes: mediaMaxProof, Roles: mediaRoles("customer", "technician", "isp", "admin", "both")},
}

// MediaHandler issues Cloudinary upload signatures scoped to the authenticated
// user's folder. File bytes never pass through the Go API; clients upload
// directly to Cloudinary and then submit the returned secure_url through
// existing *_urls fields.
type MediaHandler struct {
	cloudName         string
	apiKey            string
	apiSecret         string
	folderPrefix      string
	cloudinaryBaseURL string
	client            *http.Client
	now               func() time.Time
}

func NewMediaHandler() *MediaHandler {
	return newMediaHandler(
		os.Getenv("CLOUDINARY_CLOUD_NAME"),
		os.Getenv("CLOUDINARY_API_KEY"),
		os.Getenv("CLOUDINARY_API_SECRET"),
		os.Getenv("CLOUDINARY_FOLDER_PREFIX"),
	)
}

func newMediaHandler(cloudName, apiKey, apiSecret, folderPrefix string) *MediaHandler {
	return &MediaHandler{
		cloudName:         strings.TrimSpace(cloudName),
		apiKey:            strings.TrimSpace(apiKey),
		apiSecret:         strings.TrimSpace(apiSecret),
		folderPrefix:      sanitizeFolderPath(folderPrefix),
		cloudinaryBaseURL: "https://api.cloudinary.com",
		client:            &http.Client{Timeout: 45 * time.Second},
		now:               time.Now,
	}
}

func (h *MediaHandler) configured() bool {
	return validCloudName(h.cloudName) && h.apiKey != "" && h.apiSecret != ""
}

func (h *MediaHandler) uploadURL() string {
	return strings.TrimRight(h.cloudinaryBaseURL, "/") + "/v1_1/" + h.cloudName + "/image/upload"
}

// Signature returns the parameters Flutter or browser JavaScript must send
// with the file to Cloudinary. The API key is public. The API secret is never
// returned or logged.
func (h *MediaHandler) Signature(w http.ResponseWriter, r *http.Request) {
	if h == nil || !h.configured() {
		respondWithErrorRaw(w, http.StatusServiceUnavailable, "media storage is not configured")
		return
	}
	userID := userIDFromContext(r.Context())
	if userID == "" {
		respondWithErrorRaw(w, http.StatusUnauthorized, "authentication required")
		return
	}
	role := userRoleFromContext(r.Context())

	var request struct {
		Kind        string `json:"kind"`
		FileSize    int64  `json:"file_size"`
		ContentType string `json:"content_type"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	kindName := strings.ToLower(strings.TrimSpace(request.Kind))
	kind, ok := mediaKinds[kindName]
	if !ok {
		respondWithErrorRaw(w, http.StatusBadRequest, "unknown media kind")
		return
	}
	if !kind.Roles[role] {
		respondWithErrorRaw(w, http.StatusForbidden, "your account is not allowed to upload this media kind")
		return
	}
	if request.FileSize < 0 {
		respondWithErrorRaw(w, http.StatusBadRequest, "file_size cannot be negative")
		return
	}
	if request.FileSize > kind.MaxBytes {
		respondWithErrorRaw(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file exceeds the %d MB limit for %s", kind.MaxBytes/(1<<20), kindName))
		return
	}
	if contentType := normalizeContentType(request.ContentType); contentType != "" && !allowedMediaMIME[contentType] {
		respondWithErrorRaw(w, http.StatusBadRequest, "content_type must be a supported image type")
		return
	}

	timestamp := h.now().Unix()
	folder := h.folderPrefix + "/" + kind.Folder + "/" + userID
	params := map[string]string{
		"allowed_formats": mediaAllowedFormats,
		"folder":          folder,
		"timestamp":       strconv.FormatInt(timestamp, 10),
	}
	signature := cloudinarySignature(params, h.apiSecret)

	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data: map[string]any{
			"kind":            kindName,
			"cloud_name":      h.cloudName,
			"upload_url":      h.uploadURL(),
			"folder":          folder,
			"timestamp":       timestamp,
			"expires_at":      timestamp + 600,
			"max_file_size":   kind.MaxBytes,
			"allowed_formats": strings.Split(mediaAllowedFormats, ","),
			"upload_params": map[string]string{
				"api_key":         h.apiKey,
				"allowed_formats": mediaAllowedFormats,
				"folder":          folder,
				"signature":       signature,
				"timestamp":       strconv.FormatInt(timestamp, 10),
			},
		},
	})
}

// Upload is the server-side fallback for clients that cannot upload directly
// to Cloudinary (for example a browser blocked by a corporate proxy). It reads
// one multipart file, signs the same kind-scoped request, and forwards it to
// Cloudinary. The API secret and raw Cloudinary response headers stay server
// side.
func (h *MediaHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if h == nil || !h.configured() {
		respondWithErrorRaw(w, http.StatusServiceUnavailable, "media storage is not configured")
		return
	}
	userID := userIDFromContext(r.Context())
	if userID == "" {
		respondWithErrorRaw(w, http.StatusUnauthorized, "authentication required")
		return
	}
	role := userRoleFromContext(r.Context())

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

	kindName := strings.ToLower(strings.TrimSpace(r.FormValue("kind")))
	kind, ok := mediaKinds[kindName]
	if !ok {
		respondWithErrorRaw(w, http.StatusBadRequest, "unknown media kind")
		return
	}
	if !kind.Roles[role] {
		respondWithErrorRaw(w, http.StatusForbidden, "your account is not allowed to upload this media kind")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondWithErrorRaw(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	if header.Size > kind.MaxBytes {
		respondWithErrorRaw(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file exceeds the %d MB limit for %s", kind.MaxBytes/(1<<20), kindName))
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, kind.MaxBytes+1))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	if int64(len(data)) > kind.MaxBytes {
		respondWithErrorRaw(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file exceeds the %d MB limit for %s", kind.MaxBytes/(1<<20), kindName))
		return
	}
	if len(data) == 0 {
		respondWithErrorRaw(w, http.StatusBadRequest, "uploaded file is empty")
		return
	}
	filename := safeUploadFilename(header.Filename)
	contentType := http.DetectContentType(data)
	if !allowedMediaMIME[contentType] && !heicFilename(filename) {
		respondWithErrorRaw(w, http.StatusBadRequest, "file must be a supported image type")
		return
	}

	timestamp := h.now().Unix()
	folder := h.folderPrefix + "/" + kind.Folder + "/" + userID
	signed := map[string]string{
		"allowed_formats": mediaAllowedFormats,
		"folder":          folder,
		"timestamp":       strconv.FormatInt(timestamp, 10),
	}
	params := map[string]string{
		"api_key":         h.apiKey,
		"allowed_formats": mediaAllowedFormats,
		"folder":          folder,
		"signature":       cloudinarySignature(signed, h.apiSecret),
		"timestamp":       strconv.FormatInt(timestamp, 10),
	}
	body, multipartType, err := buildCloudinaryUpload(data, filename, params)
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.uploadURL(), bytes.NewReader(body))
	if err != nil {
		respondWithInternalError(w, err)
		return
	}
	req.Header.Set("Content-Type", multipartType)
	req.ContentLength = int64(len(body))
	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("cloudinary fallback upload failed: %v", err)
		respondWithErrorRaw(w, http.StatusBadGateway, "media upload failed")
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		log.Printf("cloudinary fallback upload rejected status=%d body=%s", resp.StatusCode, truncateForLog(raw, 500))
		respondWithErrorRaw(w, http.StatusBadGateway, "media upload failed")
		return
	}
	var uploaded struct {
		SecureURL    string `json:"secure_url"`
		PublicID     string `json:"public_id"`
		Format       string `json:"format"`
		Bytes        int64  `json:"bytes"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		ResourceType string `json:"resource_type"`
	}
	if err := json.Unmarshal(raw, &uploaded); err != nil || uploaded.SecureURL == "" {
		log.Printf("cloudinary fallback upload returned an invalid response: %s", truncateForLog(raw, 500))
		respondWithErrorRaw(w, http.StatusBadGateway, "media upload failed")
		return
	}
	respondWithJSON(w, http.StatusOK, dto.APIResponse{
		Success: true,
		Data: map[string]any{
			"kind":          kindName,
			"secure_url":    uploaded.SecureURL,
			"public_id":     uploaded.PublicID,
			"format":        uploaded.Format,
			"bytes":         uploaded.Bytes,
			"width":         uploaded.Width,
			"height":        uploaded.Height,
			"resource_type": uploaded.ResourceType,
			"uploaded_via":  "server_fallback",
		},
	})
}

func buildCloudinaryUpload(file []byte, filename string, params map[string]string) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, key := range sortedParamKeys(params) {
		if err := writer.WriteField(key, params[key]); err != nil {
			return nil, "", err
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(file); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
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

func heicFilename(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".heic", ".heif":
		return true
	default:
		return false
	}
}

func truncateForLog(value []byte, limit int) string {
	if len(value) > limit {
		value = value[:limit]
	}
	return string(value)
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

func validCloudName(value string) bool {
	if value == "" || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func sanitizeFolderPath(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "ispilolite"
	}
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(value, "/") {
		var b strings.Builder
		for _, r := range strings.TrimSpace(part) {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
				b.WriteRune(r)
			default:
				b.WriteByte('-')
			}
		}
		cleaned := strings.Trim(b.String(), "-.")
		if cleaned != "" {
			parts = append(parts, cleaned)
		}
	}
	if len(parts) == 0 {
		return "ispilolite"
	}
	return strings.Join(parts, "/")
}

// cloudinarySignature follows Cloudinary's signed-upload contract: sort all
// signed parameters by key, join them as key=value with &, append the API
// secret, then SHA-1 the result. Only image uploads are signed here.
func cloudinarySignature(params map[string]string, apiSecret string) string {
	keys := sortedParamKeys(params)
	var payload strings.Builder
	for i, key := range keys {
		if i > 0 {
			payload.WriteByte('&')
		}
		payload.WriteString(key)
		payload.WriteByte('=')
		payload.WriteString(params[key])
	}
	payload.WriteString(apiSecret)
	sum := sha1.Sum([]byte(payload.String()))
	return hex.EncodeToString(sum[:])
}

func sortedParamKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

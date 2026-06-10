package oneedu

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"admin-bot/internal/domain"
)

const (
	storagePath = "/api/storage"

	// maxPhotoBytes caps a single downloaded photo. Generously larger than
	// maxResponseBytes (which guards JSON payloads) because uploaded ID photos
	// can legitimately be several megabytes.
	maxPhotoBytes = 20 << 20 // 20 MiB
)

// contentTypeExt maps the photo Content-Types the platform serves to file
// extensions, mirroring the reference script. Unknown types fall back to .jpg.
var contentTypeExt = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// GetUserByLogin fetches a single user by login and extracts the identity
// fields needed for a Face-ID report. Returns (nil, nil) when no user matches.
func (c *Client) GetUserByLogin(ctx context.Context, login string) (*domain.FaceUser, error) {
	vars := map[string]interface{}{"login": login}

	var resp userResponse
	if err := c.runQuery(ctx, "GetUserByLogin", vars, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data.User) == 0 {
		return nil, nil
	}

	u := resp.Data.User[0]
	return &domain.FaceUser{
		Login:         login,
		LastNameCyr:   attrString(u.Attrs, "lastNameCyr"),
		FirstNameCyr:  attrString(u.Attrs, "firstNameCyr"),
		IIN:           firstNonEmpty(attrString(u.Attrs, "IIN"), attrString(u.Attrs, "iin")),
		PhotoUploadID: attrString(u.Attrs, "photoUploadId"),
	}, nil
}

// DownloadPhoto fetches the photo bytes for a given storage upload ID.
//
// The platform's storage endpoint takes the JWT in the query string
// (?token=...), exactly like the reference Python script. As with
// requestToken, that means the token can leak into *url.Error on a transport
// failure, so we redact it with scrubURLError before returning.
func (c *Client) DownloadPhoto(ctx context.Context, photoUploadID string) ([]byte, string, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, "", err
	}
	token, _ := c.tokenState()

	endpoint := c.baseURL + storagePath + "?" + url.Values{
		"fileId": {photoUploadID},
		"token":  {token},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", scrubURLError(err, token)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("storage endpoint returned status %d for fileId %q", resp.StatusCode, photoUploadID)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoBytes))
	if err != nil {
		return nil, "", fmt.Errorf("read photo body: %w", err)
	}

	ext := extForContentType(resp.Header.Get("Content-Type"))
	return data, ext, nil
}

// extForContentType maps a Content-Type header (which may carry parameters such
// as "; charset=...") to a file extension, defaulting to .jpg.
func extForContentType(contentType string) string {
	ct := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	if ext, ok := contentTypeExt[strings.ToLower(ct)]; ok {
		return ext
	}
	return ".jpg"
}

// attrString reads a string-ish value from a jsonb attrs map. JSON numbers
// decode to float64, so an IIN stored as a number is rendered without a
// decimal point or exponent.
func attrString(attrs map[string]interface{}, key string) string {
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

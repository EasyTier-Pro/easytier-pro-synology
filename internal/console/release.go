package console

import (
	"context"
	"net/http"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
)

// LatestRelease reads the public release channels and the configuration server
// address this device must use.
func (c *Client) LatestRelease(ctx context.Context) (Release, *apperr.Error) {
	status, body, aerr := c.publicCall(ctx, http.MethodGet, "/api/v1/releases/latest", nil, "application/json")
	if aerr != nil {
		return Release{}, aerr
	}
	if status != http.StatusOK {
		return Release{}, apperr.New(apperr.CodeReleaseLookupFailed)
	}
	var release Release
	if aerr := decodeJSON(body, &release); aerr != nil {
		return Release{}, apperr.New(apperr.CodeReleaseLookupFailed)
	}
	return release, nil
}

// ConfigServerURL resolves the configuration server for this device. Console
// may leave it empty, in which case the caller reports an invalid address.
func (r Release) ConfigServerURL() string {
	return r.WebConfigServerURL
}

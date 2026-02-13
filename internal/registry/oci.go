//go:build containers_image_openpgp && containers_image_storage_stub && containers_image_docker_daemon_stub

package registry

import (
	"encoding/json/v2"
	"fmt"
	"io"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// parseOCIConfig parses an OCI image config blob.
func parseOCIConfig(data []byte) (*imgspecv1.Image, error) {
	var img imgspecv1.Image
	if err := json.Unmarshal(data, &img); err != nil {
		return nil, fmt.Errorf("parse OCI config: %w", err)
	}
	return &img, nil
}

// extractHasHealthcheck checks whether the raw config blob defines a Docker
// HEALTHCHECK. The OCI image spec (imgspecv1.ImageConfig) does not include the
// Healthcheck field, but Docker image configs on registries do. We parse only
// the relevant nested field from the raw JSON to detect it.
func extractHasHealthcheck(configBytes []byte) bool {
	var dockerCfg struct {
		Config struct {
			Healthcheck *struct {
				Test []string `json:",omitempty"`
			} `json:"Healthcheck,omitempty"`
		} `json:"config"`
	}
	if err := json.Unmarshal(configBytes, &dockerCfg); err != nil {
		return false
	}
	hc := dockerCfg.Config.Healthcheck
	return hc != nil && len(hc.Test) > 0 && hc.Test[0] != "NONE"
}

// readAll reads up to maxBytes from r.
func readAll(r io.Reader, maxBytes int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxBytes))
}

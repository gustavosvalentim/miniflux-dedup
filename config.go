package dedup

import (
	"errors"
	"net/url"
	"strings"
)

const (
	minifluxURLEnv    = "MINIFLUX_URL"
	minifluxAPIKeyEnv = "MINIFLUX_API_KEY"
	minifluxTokenEnv  = "MINIFLUX_API_TOKEN"
)

// Config contains the credentials and endpoint needed to access Miniflux.
type Config struct {
	BaseURL  *url.URL
	APIToken string
}

// LoadConfig reads and validates configuration without including secret values
// in returned errors.
func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("configuration reader is required")
	}

	rawURL := strings.TrimSpace(getenv(minifluxURLEnv))
	if rawURL == "" {
		return Config{}, errors.New("MINIFLUX_URL is required")
	}

	baseURL, err := url.Parse(rawURL)
	if err != nil || baseURL.Host == "" || baseURL.User != nil ||
		(baseURL.Scheme != "http" && baseURL.Scheme != "https") ||
		baseURL.RawQuery != "" || baseURL.ForceQuery || baseURL.Fragment != "" {
		return Config{}, errors.New("MINIFLUX_URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")

	token := strings.TrimSpace(getenv(minifluxAPIKeyEnv))
	if token == "" {
		token = strings.TrimSpace(getenv(minifluxTokenEnv))
	}
	if token == "" {
		return Config{}, errors.New("MINIFLUX_API_KEY is required (MINIFLUX_API_TOKEN is accepted as a legacy alias)")
	}

	config := Config{BaseURL: baseURL, APIToken: token}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateConfig(config Config) error {
	if config.BaseURL == nil || config.BaseURL.Host == "" || config.BaseURL.User != nil ||
		(config.BaseURL.Scheme != "http" && config.BaseURL.Scheme != "https") ||
		config.BaseURL.RawQuery != "" || config.BaseURL.ForceQuery || config.BaseURL.Fragment != "" {
		return errors.New("MINIFLUX_URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if strings.TrimSpace(config.APIToken) == "" {
		return errors.New("MINIFLUX_API_KEY is required (MINIFLUX_API_TOKEN is accepted as a legacy alias)")
	}
	return nil
}

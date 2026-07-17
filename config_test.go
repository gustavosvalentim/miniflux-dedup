package dedup

import (
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		minifluxURLEnv:    " https://reader.example.com/miniflux/ ",
		minifluxAPIKeyEnv: " secret-key ",
	}
	config, err := LoadConfig(func(name string) string { return environment[name] })
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got, want := config.BaseURL.String(), "https://reader.example.com/miniflux"; got != want {
		t.Errorf("BaseURL = %q, want %q", got, want)
	}
	if got, want := config.APIToken, "secret-key"; got != want {
		t.Errorf("APIToken = %q, want %q", got, want)
	}
}

func TestLoadConfigAcceptsLegacyTokenAndPrefersAPIKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
		want        string
	}{
		{
			name: "legacy token",
			environment: map[string]string{
				minifluxURLEnv:   "https://reader.example.com",
				minifluxTokenEnv: "legacy-token",
			},
			want: "legacy-token",
		},
		{
			name: "API key takes precedence",
			environment: map[string]string{
				minifluxURLEnv:    "https://reader.example.com",
				minifluxAPIKeyEnv: "primary-key",
				minifluxTokenEnv:  "legacy-token",
			},
			want: "primary-key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, err := LoadConfig(func(name string) string { return test.environment[name] })
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if config.APIToken != test.want {
				t.Errorf("APIToken = %q, want %q", config.APIToken, test.want)
			}
		})
	}
}

func TestLoadConfigRejectsInvalidValuesWithoutLeakingToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
		wantMessage string
	}{
		{
			name:        "nil reader",
			wantMessage: "configuration reader is required",
		},
		{
			name: "missing URL",
			environment: map[string]string{
				minifluxTokenEnv: "top-secret",
			},
			wantMessage: "MINIFLUX_URL is required",
		},
		{
			name: "relative URL",
			environment: map[string]string{
				minifluxURLEnv:   "/relative",
				minifluxTokenEnv: "top-secret",
			},
			wantMessage: "MINIFLUX_URL must be an absolute HTTP(S) URL",
		},
		{
			name: "URL credentials",
			environment: map[string]string{
				minifluxURLEnv:   "https://user:password@example.com",
				minifluxTokenEnv: "top-secret",
			},
			wantMessage: "MINIFLUX_URL must be an absolute HTTP(S) URL",
		},
		{
			name: "URL query",
			environment: map[string]string{
				minifluxURLEnv:   "https://example.com?token=other-secret",
				minifluxTokenEnv: "top-secret",
			},
			wantMessage: "MINIFLUX_URL must be an absolute HTTP(S) URL",
		},
		{
			name: "empty URL query",
			environment: map[string]string{
				minifluxURLEnv:   "https://example.com?",
				minifluxTokenEnv: "top-secret",
			},
			wantMessage: "MINIFLUX_URL must be an absolute HTTP(S) URL",
		},
		{
			name: "missing API token",
			environment: map[string]string{
				minifluxURLEnv: "https://example.com",
			},
			wantMessage: "MINIFLUX_API_KEY is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var getenv func(string) string
			if test.environment != nil {
				getenv = func(name string) string { return test.environment[name] }
			}
			_, err := LoadConfig(getenv)
			if err == nil {
				t.Fatal("LoadConfig() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("error = %q, want it to contain %q", err, test.wantMessage)
			}
			if strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), "other-secret") {
				t.Errorf("error leaked a secret: %q", err)
			}
		})
	}
}

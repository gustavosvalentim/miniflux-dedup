package dedup_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCronEnvironmentExampleExportsConfiguration(t *testing.T) {
	t.Parallel()

	command := exec.Command(
		"/bin/sh",
		"-c",
		`. ./deploy/miniflux-dedup.env.example && printf '%s\n%s\n' "$MINIFLUX_URL" "$MINIFLUX_API_KEY"`,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("source cron environment: %v\n%s", err, output)
	}
	values := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(values) != 2 || values[0] != "https://reader.example.com" || values[1] != "replace-with-your-api-key" {
		t.Errorf("exported example values = %q, want URL and API key", values)
	}
}

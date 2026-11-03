package push

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// The API and worker only see deployment variables that Compose forwards into
// their containers. A missing entry here makes the API report {enabled:false}
// even when finance.env has the keys.
func TestComposeForwardsVAPIDToAPIAndWorker(t *testing.T) {
	raw, err := os.ReadFile("../../../../compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"api", "worker"} {
		for _, key := range []string{"VAPID_PRIVATE_KEY", "VAPID_SUBJECT"} {
			want := "${" + key + ":-}"
			if got := compose.Services[service].Environment[key]; got != want {
				t.Fatalf("compose service %s %s=%q, want %q", service, key, got, want)
			}
		}
	}
}

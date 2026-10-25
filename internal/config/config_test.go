package config_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/config"
)

// env is a fake environment.
func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestConfigIsReadFromTheEnvironment(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{
		"PORT":                     "8080",
		"DATABASE_PATH":            "data/go-slow.db",
		"COLLECTION_CONTACT":       "owner@example.org",
		"COLLECTION_INTERVAL":      "15m",
		"READING_MODEL":            "claude-haiku-5-5",
		"PLACING_MODEL":            "claude-sonnet-5-5",
		"GOSLOW_ANTHROPIC_API_KEY": "sk-test",
	}))
	if err != nil {
		t.Fatal(err)
	}

	want := config.Config{
		Port:               8080,
		DatabasePath:       "data/go-slow.db",
		CollectionContact:  "owner@example.org",
		CollectionInterval: 15 * time.Minute,
		ReadingModel:       "claude-haiku-5-5",
		PlacingModel:       "claude-sonnet-5-5",
		AnthropicAPIKey:    "sk-test",
	}
	if cfg != want {
		t.Errorf("Load = %+v, want %+v", cfg, want)
	}
}

func TestMissingVariablesAreAllNamed(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_PATH": ""}))

	if err == nil {
		t.Fatal("Load succeeded with nothing set")
	}
	for _, name := range []string{"PORT", "DATABASE_PATH", "COLLECTION_CONTACT", "COLLECTION_INTERVAL", "READING_MODEL", "PLACING_MODEL", "GOSLOW_ANTHROPIC_API_KEY"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestPortMustBeANumber(t *testing.T) {
	vars := valid()
	vars["PORT"] = "eighty"
	_, err := config.Load(env(vars))

	if err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Errorf("Load error = %v, want one naming PORT", err)
	}
}

func TestCollectionIntervalMustBeAPositiveDuration(t *testing.T) {
	for _, bad := range []string{"often", "0s", "-5m"} {
		vars := valid()
		vars["COLLECTION_INTERVAL"] = bad
		_, err := config.Load(env(vars))

		if err == nil || !strings.Contains(err.Error(), "COLLECTION_INTERVAL") {
			t.Errorf("Load(%q) error = %v, want one naming COLLECTION_INTERVAL", bad, err)
		}
	}
}

func valid() map[string]string {
	return map[string]string{
		"PORT":                     "8080",
		"DATABASE_PATH":            "x.db",
		"COLLECTION_CONTACT":       "owner@example.org",
		"COLLECTION_INTERVAL":      "15m",
		"READING_MODEL":            "claude-haiku-5-5",
		"PLACING_MODEL":            "claude-sonnet-5-5",
		"GOSLOW_ANTHROPIC_API_KEY": "sk-test",
	}
}

func TestEachActsModelMustBeAClaudeModel(t *testing.T) {
	// Only the Claude adapter exists yet; another provider's model would be
	// sent to Anthropic's API and fail on every call.
	for _, name := range []string{"READING_MODEL", "PLACING_MODEL"} {
		vars := valid()
		vars[name] = "glm-5.3-flash"
		_, err := config.Load(env(vars))

		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("Load error = %v, want one naming %s", err, name)
		}
	}
}

func TestTheAPIKeyIsNeverShownInTheConfig(t *testing.T) {
	cfg, err := config.Load(env(valid()))
	if err != nil {
		t.Fatal(err)
	}

	if s := fmt.Sprintf("%v %+v", cfg, cfg); strings.Contains(s, "sk-test") {
		t.Errorf("formatting the config shows the key: %s", s)
	}
}

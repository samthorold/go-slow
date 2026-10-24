package config_test

import (
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
		"PORT":                "8080",
		"DATABASE_PATH":       "data/go-slow.db",
		"COLLECTION_CONTACT":  "owner@example.org",
		"COLLECTION_INTERVAL": "15m",
	}))
	if err != nil {
		t.Fatal(err)
	}

	want := config.Config{
		Port:               8080,
		DatabasePath:       "data/go-slow.db",
		CollectionContact:  "owner@example.org",
		CollectionInterval: 15 * time.Minute,
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
	for _, name := range []string{"PORT", "DATABASE_PATH", "COLLECTION_CONTACT", "COLLECTION_INTERVAL"} {
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
		"PORT":                "8080",
		"DATABASE_PATH":       "x.db",
		"COLLECTION_CONTACT":  "owner@example.org",
		"COLLECTION_INTERVAL": "15m",
	}
}

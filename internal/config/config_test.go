package config_test

import (
	"strings"
	"testing"

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
		"PORT":          "8080",
		"DATABASE_PATH": "data/go-slow.db",
	}))
	if err != nil {
		t.Fatal(err)
	}

	want := config.Config{Port: 8080, DatabasePath: "data/go-slow.db"}
	if cfg != want {
		t.Errorf("Load = %+v, want %+v", cfg, want)
	}
}

func TestMissingVariablesAreAllNamed(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_PATH": ""}))

	if err == nil {
		t.Fatal("Load succeeded with nothing set")
	}
	for _, name := range []string{"PORT", "DATABASE_PATH"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestPortMustBeANumber(t *testing.T) {
	_, err := config.Load(env(map[string]string{"PORT": "eighty", "DATABASE_PATH": "x.db"}))

	if err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Errorf("Load error = %v, want one naming PORT", err)
	}
}

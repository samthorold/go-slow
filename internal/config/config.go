// Package config is the app's typed configuration, read once from the
// environment by main. Every variable is listed in .env.example.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config is everything that varies between deploys.
type Config struct {
	// Port is the TCP port the app listens on (PORT).
	Port int
	// DatabasePath is the SQLite file holding the Record (DATABASE_PATH).
	DatabasePath string
	// CollectionContact is the contact address Collection names in its
	// User-Agent, so a publisher can reach whoever runs it
	// (COLLECTION_CONTACT).
	CollectionContact string
	// CollectionInterval is how often Collection runs (COLLECTION_INTERVAL,
	// a Go duration such as 15m).
	CollectionInterval time.Duration
	// ReadingModel is the Claude model the Editor reads Items with
	// (READING_MODEL, such as claude-haiku-5-5).
	ReadingModel string
	// AnthropicAPIKey is the key for Anthropic's API (GOSLOW_ANTHROPIC_API_KEY).
	AnthropicAPIKey Secret
}

// Secret is a credential: formatting it shows nothing of it, so a logged or
// printed Config never leaks it.
type Secret string

func (Secret) String() string   { return "[secret]" }
func (Secret) GoString() string { return `"[secret]"` }

// Load reads the config through lookup (os.LookupEnv in main). Every
// variable is required: the safe local values live in .env.example, not in
// code, so a deploy that forgets one fails at start rather than running on a
// default.
func Load(lookup func(string) (string, bool)) (Config, error) {
	var missing []string
	get := func(key string) string {
		v, ok := lookup(key)
		if !ok || v == "" {
			missing = append(missing, key)
		}
		return v
	}
	port, dbPath := get("PORT"), get("DATABASE_PATH")
	contact, interval := get("COLLECTION_CONTACT"), get("COLLECTION_INTERVAL")
	model, key := get("READING_MODEL"), get("GOSLOW_ANTHROPIC_API_KEY")
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}

	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Config{}, errors.New("PORT must be a TCP port number, got " + strconv.Quote(port))
	}
	every, err := time.ParseDuration(interval)
	if err != nil || every <= 0 {
		return Config{}, errors.New("COLLECTION_INTERVAL must be a positive duration such as 15m, got " + strconv.Quote(interval))
	}
	// Only the Claude adapter exists yet (open-weight models come through
	// OpenRouter later), so any other model would fail on every reading.
	if !strings.HasPrefix(model, "claude-") {
		return Config{}, errors.New("READING_MODEL must be a Claude model such as claude-haiku-5-5, got " + strconv.Quote(model))
	}
	return Config{
		Port:               n,
		DatabasePath:       dbPath,
		CollectionContact:  contact,
		CollectionInterval: every,
		ReadingModel:       model,
		AnthropicAPIKey:    Secret(key),
	}, nil
}

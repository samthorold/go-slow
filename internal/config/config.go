// Package config is the app's typed configuration, read once from the
// environment by main. Every variable is listed in .env.example.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Config is everything that varies between deploys.
type Config struct {
	// Port is the TCP port the app listens on (PORT).
	Port int
	// DatabasePath is the SQLite file holding the Record (DATABASE_PATH).
	DatabasePath string
}

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
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}

	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Config{}, errors.New("PORT must be a TCP port number, got " + strconv.Quote(port))
	}
	return Config{Port: n, DatabasePath: dbPath}, nil
}

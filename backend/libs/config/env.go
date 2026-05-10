package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func String(key, defaultValue string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return defaultValue
}

func RequiredString(key string) (string, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return "", fmt.Errorf("missing required env var %q", key)
	}
	return v, nil
}

func Bool(key string, defaultValue bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

func Int(key string, defaultValue int) int {
	if v, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

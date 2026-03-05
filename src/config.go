package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Config holds all run-time configuration for the server.
type Config struct {
	UUID            string
	Port            string
	TunnelDomain    string
	Name            string
	VlessPathPrefix string
}

var (
	uuidRegex   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	domainRegex = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9]$`)
)

func isValidUUID(v string) bool {
	return uuidRegex.MatchString(v)
}

func isValidPort(v string) bool {
	p, err := strconv.Atoi(strings.TrimSpace(v))
	return err == nil && p >= 1 && p <= 65535
}

func isValidDomain(v string) bool {
	return len(v) <= 255 && domainRegex.MatchString(v)
}

// getEnvVariable reads an environment variable, validates it, and falls back to
// defaultValue when the variable is unset. Returns an error when the value (or
// default) fails validation or when no value is available at all.
func getEnvVariable(name string, validator func(string) bool, defaultValue string) (string, error) {
	value := os.Getenv(name)
	if value != "" {
		if !validator(value) {
			return "", fmt.Errorf("invalid value for environment variable %s: %s", name, value)
		}
		return value, nil
	}
	if defaultValue != "" {
		if !validator(defaultValue) {
			return "", fmt.Errorf("invalid default value for %s: %s", name, defaultValue)
		}
		return defaultValue, nil
	}
	return "", fmt.Errorf("missing required environment variable: %s", name)
}

func loadConfig() (*Config, error) {
	uuid, err := getEnvVariable("UUID", isValidUUID, "")
	if err != nil {
		return nil, err
	}

	port, err := getEnvVariable("PORT", isValidPort, "3182")
	if err != nil {
		return nil, err
	}

	tunnelDomain, err := getEnvVariable("TUNNEL_DOMAIN", isValidDomain, "")
	if err != nil {
		return nil, err
	}

	name := os.Getenv("NAME")
	if name == "" {
		name, err = os.Hostname()
		if err != nil {
			name = "unknown"
		}
	}

	vlessPathPrefix := os.Getenv("VLESS_PATH_PREFIX")
	if vlessPathPrefix == "" {
		vlessPathPrefix = "/work-tunnel-"
	}

	return &Config{
		UUID:            uuid,
		Port:            port,
		TunnelDomain:    tunnelDomain,
		Name:            name,
		VlessPathPrefix: vlessPathPrefix,
	}, nil
}

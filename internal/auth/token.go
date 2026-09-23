package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SaveToken stores the Vault client token for later oks commands.
func SaveToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("token is empty")
	}
	path, err := tokenPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// LoadToken reads the token written by oks auth login.
func LoadToken() (string, error) {
	path, err := tokenPath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("not logged in. Run: oks auth login")
		}
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty. Run: oks auth login", path)
	}
	return token, nil
}

func tokenPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if strings.TrimSpace(dir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "oks", "token"), nil
}

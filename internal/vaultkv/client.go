package vaultkv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/vault/api"
)

const DefaultAddress = "https://vault.odrisystems.com"

// NewClient builds a Vault client. addr falls back to VAULT_ADDR, then DefaultAddress.
func NewClient(addr string) (*api.Client, error) {
	resolved := strings.TrimSpace(addr)
	if resolved == "" {
		resolved = strings.TrimSpace(os.Getenv("VAULT_ADDR"))
	}
	if resolved == "" {
		resolved = DefaultAddress
	}

	cfg := api.DefaultConfig()
	cfg.Address = resolved
	if err := cfg.ReadEnvironment(); err != nil {
		return nil, err
	}
	cfg.Address = resolved

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// ReadKV2 reads a KV v2 secret. path is the API path, for example clusters/data/name.
func ReadKV2(client *api.Client, path string) (map[string]string, error) {
	sec, err := client.Logical().Read(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	if sec == nil || sec.Data == nil {
		return nil, fmt.Errorf("no data at path %q", path)
	}
	inner, ok := sec.Data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("KV v2 response missing data.data")
	}
	out := make(map[string]string, len(inner))
	for k, v := range inner {
		out[k] = stringify(v)
	}
	return out, nil
}

func stringify(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	case []byte:
		return string(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

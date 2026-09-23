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

// ListKV2 lists secret names on a KV v2 mount. mount is the mount path, usually "clusters".
func ListKV2(client *api.Client, mount string) ([]string, error) {
	mount = strings.Trim(strings.TrimSpace(mount), "/")
	if mount == "" {
		mount = "clusters"
	}
	sec, err := client.Logical().List(mount + "/metadata")
	if err != nil {
		return nil, err
	}
	if sec == nil || sec.Data == nil {
		return nil, nil
	}
	raw, ok := sec.Data["keys"].([]interface{})
	if !ok {
		return nil, errors.New("KV v2 list response missing keys")
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(stringify(item))
		name = strings.Trim(name, "/")
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// WriteKV2 writes a KV v2 secret. path is the API path, for example clusters/data/name.
func WriteKV2(client *api.Client, path string, data map[string]string) error {
	payload := make(map[string]interface{}, len(data))
	for k, v := range data {
		payload[k] = v
	}
	_, err := client.Logical().Write(strings.TrimSpace(path), map[string]interface{}{
		"data": payload,
	})
	return err
}

// DeleteKV2 removes every version of a KV v2 secret so it no longer appears in the list.
func DeleteKV2(client *api.Client, mount, name string) error {
	mount = strings.Trim(strings.TrimSpace(mount), "/")
	name = strings.Trim(strings.TrimSpace(name), "/")
	_, err := client.Logical().Delete(mount + "/metadata/" + name)
	return err
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

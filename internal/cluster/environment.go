package cluster

import (
	"fmt"
	"strings"
)

// Environment is one OKS cluster whose kubeconfig lives in Vault KV v2.
type Environment struct {
	Name    string
	Mount   string
	Secret  string
	Cluster string
}

// Lookup returns the cluster named by the caller. Secrets live on the clusters mount.
func Lookup(name string) (Environment, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Environment{}, fmt.Errorf("cluster is required")
	}
	return Environment{
		Name:    name,
		Mount:   "clusters",
		Secret:  name,
		Cluster: name,
	}, nil
}

// APIPath is the KV v2 read path. mountOverride replaces the clusters mount when set.
func (e Environment) APIPath(mountOverride string) string {
	mount := e.Mount
	if strings.TrimSpace(mountOverride) != "" {
		mount = strings.Trim(mountOverride, "/")
	}
	return fmt.Sprintf("%s/data/%s", mount, e.Secret)
}

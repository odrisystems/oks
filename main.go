// OKS (Odri Kubernetes Service) — oks fetches kubeconfig material from HashiCorp Vault (KV v2)
// and merges it into a kubeconfig file (same approach as gcloud/aws/az get-credentials), or prints YAML.
// If the kubeconfig file does not exist yet, it is created with only the Vault material.
// If it exists, only the cluster, user, and context names present in the Vault document are updated;
// other entries and preferences are preserved.
// Auth: VAULT_ADDR and VAULT_TOKEN (or VAULT_NAMESPACE).
//
// Secret formats:
//   1) Single field: kubeconfig, config, kube_config, or content — full kubeconfig YAML.
//   2) Structured fields: server, token, certificate_authority_data (base64) OR certificate_authority (PEM);
//      optional: client_certificate_data, client_key_data (base64); namespace (optional; also configurable via -namespace).
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/vault/api"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func main() {
	cluster := flag.String("cluster", "", "Cluster name (required). Defaults kubeconfig cluster/user/context to this name; Vault path defaults to secret/data/clusters/<cluster>")
	vaultPath := flag.String("path", "", "Vault secret path override (KV v2). Default: secret/data/clusters/<cluster>")
	namespace := flag.String("namespace", "", "Namespace to set on the kubeconfig context (optional)")
	field := flag.String("field", "", "If set, read only this key as raw kubeconfig YAML (skips structured assembly)")
	outPath := flag.String("o", "", "Kubeconfig file to write. Use '-' for stdout (no merge). Empty: merge into default kubeconfig (~/.kube/config or single path from KUBECONFIG)")
	overwrite := flag.Bool("overwrite", false, "Replace the entire kubeconfig file instead of merging (only with -o path)")
	setCurrent := flag.Bool("set-current-context", true, "After merge, set current-context to the context from Vault (like gcloud get-credentials)")
	flag.Parse()

	if strings.TrimSpace(*cluster) == "" {
		fmt.Fprintln(os.Stderr, "error: -cluster is required")
		flag.Usage()
		os.Exit(2)
	}
	name := strings.TrimSpace(*cluster)
	if strings.TrimSpace(*vaultPath) == "" {
		*vaultPath = fmt.Sprintf("secret/data/clusters/%s", name)
	}

	cfg := api.DefaultConfig()
	if err := cfg.ReadEnvironment(); err != nil {
		fmt.Fprintf(os.Stderr, "vault config: %v\n", err)
		os.Exit(1)
	}
	client, err := api.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault client: %v\n", err)
		os.Exit(1)
	}
	if client.Token() == "" {
		fmt.Fprintln(os.Stderr, "error: VAULT_TOKEN (or token file) is required")
		os.Exit(1)
	}

	data, err := readVaultKVv2(client, *vaultPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault read: %v\n", err)
		os.Exit(1)
	}

	ns := strings.TrimSpace(*namespace)
	if ns == "" {
		ns = strings.TrimSpace(data["namespace"])
	}

	kubeYAML, err := materializeKubeconfig(data, *field, name, name, name, ns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kubeconfig: %v\n", err)
		os.Exit(1)
	}

	out := strings.TrimSpace(*outPath)
	if out == "" {
		out = defaultKubeconfigPath()
	}
	if out == "-" {
		if _, err := io.WriteString(os.Stdout, kubeYAML); err != nil {
			fmt.Fprintf(os.Stderr, "write stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *overwrite {
		if err := os.WriteFile(out, []byte(kubeYAML), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "write file: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := mergeIntoKubeconfig(out, kubeYAML, *setCurrent); err != nil {
		fmt.Fprintf(os.Stderr, "merge kubeconfig: %v\n", err)
		os.Exit(1)
	}
}

func defaultKubeconfigPath() string {
	if kc := os.Getenv(clientcmd.RecommendedConfigPathEnvVar); kc != "" {
		paths := filepath.SplitList(kc)
		if len(paths) == 1 && strings.TrimSpace(paths[0]) != "" {
			return paths[0]
		}
	}
	return clientcmd.RecommendedHomeFile
}

func mergeIntoKubeconfig(destPath, kubeYAML string, setCurrentContext bool) error {
	incoming, err := clientcmd.Load([]byte(kubeYAML))
	if err != nil {
		return fmt.Errorf("parse kubeconfig from Vault: %w", err)
	}

	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return err
	}

	if _, err := os.Stat(absDest); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// No kubeconfig yet: write only what Vault returned (nothing else to preserve).
		if setCurrentContext && incoming.CurrentContext == "" {
			incoming.CurrentContext = soleContextName(incoming)
		}
		return clientcmd.WriteToFile(*incoming, absDest)
	}

	pathOpts := clientcmd.NewDefaultPathOptions()
	pathOpts.LoadingRules.ExplicitPath = absDest

	starting, err := pathOpts.GetStartingConfig()
	if err != nil {
		return fmt.Errorf("load existing kubeconfig: %w", err)
	}

	merged := overlayIncomingStanzas(starting, incoming, setCurrentContext)
	return clientcmd.ModifyConfig(pathOpts, *merged, false)
}

// overlayIncomingStanzas copies the existing kubeconfig, then replaces only the
// cluster, user, and context entries that appear in incoming (same names as gcloud get-credentials).
// Other clusters, contexts, users, and preferences are left unchanged.
func overlayIncomingStanzas(base, incoming *clientcmdapi.Config, setCurrentContext bool) *clientcmdapi.Config {
	out := clientcmdapi.NewConfig()
	out.Preferences = base.Preferences
	out.Extensions = base.Extensions
	out.CurrentContext = base.CurrentContext

	copyClusterMap(out.Clusters, base.Clusters)
	copyAuthMap(out.AuthInfos, base.AuthInfos)
	copyContextMap(out.Contexts, base.Contexts)

	copyClusterMap(out.Clusters, incoming.Clusters)
	copyAuthMap(out.AuthInfos, incoming.AuthInfos)
	copyContextMap(out.Contexts, incoming.Contexts)

	if setCurrentContext && incoming.CurrentContext != "" {
		out.CurrentContext = incoming.CurrentContext
	}

	return out
}

func copyClusterMap(dst, src map[string]*clientcmdapi.Cluster) {
	if src == nil {
		return
	}
	for k, v := range src {
		if v == nil {
			continue
		}
		c := *v
		dst[k] = &c
	}
}

func copyAuthMap(dst, src map[string]*clientcmdapi.AuthInfo) {
	if src == nil {
		return
	}
	for k, v := range src {
		if v == nil {
			continue
		}
		u := *v
		dst[k] = &u
	}
}

func copyContextMap(dst, src map[string]*clientcmdapi.Context) {
	if src == nil {
		return
	}
	for k, v := range src {
		if v == nil {
			continue
		}
		x := *v
		dst[k] = &x
	}
}

func soleContextName(c *clientcmdapi.Config) string {
	if len(c.Contexts) != 1 {
		return ""
	}
	for n := range c.Contexts {
		return n
	}
	return ""
}

func readVaultKVv2(client *api.Client, path string) (map[string]string, error) {
	sec, err := client.Logical().Read(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	if sec == nil || sec.Data == nil {
		return nil, fmt.Errorf("no data at path %q", path)
	}
	inner, ok := sec.Data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("KV v2 response missing data.data (wrong path? expected secret/data/...)")
	}
	out := make(map[string]string, len(inner))
	for k, v := range inner {
		out[k] = stringifyVaultValue(v)
	}
	return out, nil
}

func stringifyVaultValue(v interface{}) string {
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

func materializeKubeconfig(data map[string]string, singleField, clusterName, userName, contextName, namespace string) (string, error) {
	if singleField != "" {
		s, ok := data[singleField]
		if !ok || strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("field %q missing or empty", singleField)
		}
		return strings.TrimSpace(s) + "\n", nil
	}
	for _, k := range []string{"kubeconfig", "config", "kube_config", "content"} {
		if s := strings.TrimSpace(data[k]); s != "" {
			return s + "\n", nil
		}
	}
	return assembleKubeconfig(data, clusterName, userName, contextName, namespace)
}

func assembleKubeconfig(data map[string]string, cluster, user, ctx, namespace string) (string, error) {
	server := strings.TrimSpace(data["server"])
	if server == "" {
		return "", errors.New("structured secret requires server (or a full kubeconfig in kubeconfig/config/content)")
	}
	caB64 := strings.TrimSpace(data["certificate_authority_data"])
	if pem := strings.TrimSpace(data["certificate_authority"]); pem != "" && caB64 == "" {
		caB64 = base64.StdEncoding.EncodeToString([]byte(pem))
	}
	if caB64 == "" {
		return "", errors.New("structured secret requires certificate_authority_data or certificate_authority (PEM)")
	}
	token := strings.TrimSpace(data["token"])
	clientCert := strings.TrimSpace(data["client_certificate_data"])
	clientKey := strings.TrimSpace(data["client_key_data"])
	if token == "" && (clientCert == "" || clientKey == "") {
		return "", errors.New("structured secret requires token or both client_certificate_data and client_key_data")
	}

	userObj := map[string]interface{}{}
	if token != "" {
		userObj["token"] = token
	}
	if clientCert != "" {
		userObj["client-certificate-data"] = clientCert
	}
	if clientKey != "" {
		userObj["client-key-data"] = clientKey
	}

	contextObj := map[string]interface{}{
		"cluster": cluster,
		"user":    user,
	}
	if strings.TrimSpace(namespace) != "" {
		contextObj["namespace"] = strings.TrimSpace(namespace)
	}

	doc := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Config",
		"clusters": []interface{}{
			map[string]interface{}{
				"name": cluster,
				"cluster": map[string]interface{}{
					"certificate-authority-data": caB64,
					"server":                     server,
				},
			},
		},
		"contexts": []interface{}{
			map[string]interface{}{
				"name": ctx,
				"context": contextObj,
			},
		},
		"current-context": ctx,
		"users": []interface{}{
			map[string]interface{}{
				"name": user,
				"user": userObj,
			},
		},
	}
	b, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

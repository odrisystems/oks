package kube

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// DefaultPath is KUBECONFIG when it names a single file, otherwise ~/.kube/config.
func DefaultPath() string {
	if kc := os.Getenv(clientcmd.RecommendedConfigPathEnvVar); kc != "" {
		paths := filepath.SplitList(kc)
		if len(paths) == 1 && strings.TrimSpace(paths[0]) != "" {
			return paths[0]
		}
	}
	return clientcmd.RecommendedHomeFile
}

// Materialize turns Vault secret fields into kubeconfig YAML.
// singleField, when set, is used as the raw document. Otherwise a full kubeconfig
// field is preferred, then structured server/CA/token fields.
func Materialize(data map[string]string, singleField, clusterName, userName, contextName, namespace string) (string, error) {
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
	return assemble(data, clusterName, userName, contextName, namespace)
}

// WriteFile replaces destPath with kubeYAML.
func WriteFile(destPath, kubeYAML string) error {
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absDest), 0o700); err != nil {
		return err
	}
	return os.WriteFile(absDest, []byte(kubeYAML), 0o600)
}

// Activate makes the cluster the current context. namespace, when set, is written onto that context.
// It returns the updated kubeconfig and the context name kubectl will use.
func Activate(kubeYAML, clusterName, namespace string) (string, string, error) {
	cfg, err := clientcmd.Load([]byte(kubeYAML))
	if err != nil {
		return "", "", fmt.Errorf("parse kubeconfig from Vault: %w", err)
	}
	name := pickContext(cfg, clusterName)
	if name == "" {
		return "", "", fmt.Errorf("kubeconfig has no context for cluster %s", clusterName)
	}
	ctx := cfg.Contexts[name]
	if ctx == nil {
		return "", "", fmt.Errorf("kubeconfig context %s is empty", name)
	}
	if ns := strings.TrimSpace(namespace); ns != "" {
		ctx.Namespace = ns
	}
	cfg.CurrentContext = name
	b, err := clientcmd.Write(*cfg)
	if err != nil {
		return "", "", err
	}
	return string(b), name, nil
}

// Merge updates only the cluster, user, and context names present in kubeYAML.
// Other entries stay. The incoming current context becomes the kubeconfig current context.
func Merge(destPath, kubeYAML string) error {
	incoming, err := clientcmd.Load([]byte(kubeYAML))
	if err != nil {
		return fmt.Errorf("parse kubeconfig from Vault: %w", err)
	}
	if incoming.CurrentContext == "" {
		incoming.CurrentContext = soleContextName(incoming)
	}
	if incoming.CurrentContext == "" {
		return errors.New("kubeconfig has no current context")
	}

	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return err
	}

	if _, err := os.Stat(absDest); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(absDest), 0o700); err != nil {
			return err
		}
		return clientcmd.WriteToFile(*incoming, absDest)
	}

	pathOpts := clientcmd.NewDefaultPathOptions()
	pathOpts.LoadingRules.ExplicitPath = absDest

	starting, err := pathOpts.GetStartingConfig()
	if err != nil {
		return fmt.Errorf("load existing kubeconfig: %w", err)
	}

	merged := overlay(starting, incoming)
	return clientcmd.ModifyConfig(pathOpts, *merged, false)
}

func overlay(base, incoming *clientcmdapi.Config) *clientcmdapi.Config {
	out := clientcmdapi.NewConfig()
	out.Preferences = base.Preferences
	out.Extensions = base.Extensions
	out.CurrentContext = incoming.CurrentContext

	copyClusters(out.Clusters, base.Clusters)
	copyAuth(out.AuthInfos, base.AuthInfos)
	copyContexts(out.Contexts, base.Contexts)

	copyClusters(out.Clusters, incoming.Clusters)
	copyAuth(out.AuthInfos, incoming.AuthInfos)
	copyContexts(out.Contexts, incoming.Contexts)
	return out
}

func pickContext(cfg *clientcmdapi.Config, clusterName string) string {
	if ctx, ok := cfg.Contexts[clusterName]; ok && ctx != nil {
		return clusterName
	}
	match := ""
	for name, ctx := range cfg.Contexts {
		if ctx == nil || ctx.Cluster != clusterName {
			continue
		}
		if match != "" {
			match = ""
			break
		}
		match = name
	}
	if match != "" {
		return match
	}
	if cfg.CurrentContext != "" {
		if ctx, ok := cfg.Contexts[cfg.CurrentContext]; ok && ctx != nil {
			return cfg.CurrentContext
		}
	}
	return soleContextName(cfg)
}

func copyClusters(dst, src map[string]*clientcmdapi.Cluster) {
	for k, v := range src {
		if v == nil {
			continue
		}
		c := *v
		dst[k] = &c
	}
}

func copyAuth(dst, src map[string]*clientcmdapi.AuthInfo) {
	for k, v := range src {
		if v == nil {
			continue
		}
		u := *v
		dst[k] = &u
	}
}

func copyContexts(dst, src map[string]*clientcmdapi.Context) {
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

func assemble(data map[string]string, cluster, user, ctx, namespace string) (string, error) {
	server := strings.TrimSpace(data["server"])
	if server == "" {
		return "", errors.New("structured secret requires server (or a full kubeconfig in the kubeconfig field)")
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
				"name":    ctx,
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

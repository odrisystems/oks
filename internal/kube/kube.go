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

// Merge updates only the cluster, user, and context names present in kubeYAML.
// Other entries stay. When setCurrentContext is true, the incoming context becomes current.
func Merge(destPath, kubeYAML string, setCurrentContext bool) error {
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
		if err := os.MkdirAll(filepath.Dir(absDest), 0o700); err != nil {
			return err
		}
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

	merged := overlay(starting, incoming, setCurrentContext)
	return clientcmd.ModifyConfig(pathOpts, *merged, false)
}

func overlay(base, incoming *clientcmdapi.Config, setCurrentContext bool) *clientcmdapi.Config {
	out := clientcmdapi.NewConfig()
	out.Preferences = base.Preferences
	out.Extensions = base.Extensions
	out.CurrentContext = base.CurrentContext

	copyClusters(out.Clusters, base.Clusters)
	copyAuth(out.AuthInfos, base.AuthInfos)
	copyContexts(out.Contexts, base.Contexts)

	copyClusters(out.Clusters, incoming.Clusters)
	copyAuth(out.AuthInfos, incoming.AuthInfos)
	copyContexts(out.Contexts, incoming.Contexts)

	if setCurrentContext && incoming.CurrentContext != "" {
		out.CurrentContext = incoming.CurrentContext
	} else if setCurrentContext {
		if name := soleContextName(incoming); name != "" {
			out.CurrentContext = name
		}
	}
	return out
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

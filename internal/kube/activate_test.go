package kube

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestActivateSwitchesCurrentContext(t *testing.T) {
	raw := `apiVersion: v1
kind: Config
current-context: other
clusters:
- name: kind-odri-cluster
  cluster:
    server: https://127.0.0.1:6443
- name: other
  cluster:
    server: https://127.0.0.1:6444
contexts:
- name: other
  context:
    cluster: other
    user: other
- name: kind-odri-cluster
  context:
    cluster: kind-odri-cluster
    user: kind-odri-cluster
users:
- name: kind-odri-cluster
  user:
    token: a
- name: other
  user:
    token: b
`
	activated, name, err := Activate(raw, "kind-odri-cluster", "workspacepro-prod")
	if err != nil {
		t.Fatal(err)
	}
	if name != "kind-odri-cluster" {
		t.Fatalf("context = %s", name)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "config")
	existing := clientcmdapi.NewConfig()
	existing.Clusters["other"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:6444"}
	existing.AuthInfos["other"] = &clientcmdapi.AuthInfo{Token: "b"}
	existing.Contexts["other"] = &clientcmdapi.Context{Cluster: "other", AuthInfo: "other"}
	existing.CurrentContext = "other"
	if err := clientcmd.WriteToFile(*existing, dest); err != nil {
		t.Fatal(err)
	}
	if err := Merge(dest, activated); err != nil {
		t.Fatal(err)
	}

	loaded, err := clientcmd.LoadFromFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentContext != "kind-odri-cluster" {
		t.Fatalf("current-context = %s", loaded.CurrentContext)
	}
	if loaded.Contexts["kind-odri-cluster"].Namespace != "workspacepro-prod" {
		t.Fatalf("namespace = %s", loaded.Contexts["kind-odri-cluster"].Namespace)
	}
	if _, ok := loaded.Contexts["other"]; !ok {
		t.Fatal("existing context was removed")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
}

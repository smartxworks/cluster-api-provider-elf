package controllers

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEnsureKubeadmConfigContentPreservesMultiDocumentInitConfiguration(t *testing.T) {
	content := newStringYAMLNode(`---
apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: phy
---
apiVersion: kubeadm.k8s.io/v1beta3
kind: InitConfiguration
nodeRegistration:
  kubeletExtraArgs:
    event-qps: "0"
---
apiVersion: kubeproxy.config.k8s.io/v1alpha1
kind: KubeProxyConfiguration
metricsBindAddress: 0.0.0.0:10249
`)

	changed, err := ensureKubeadmConfigContent(content, "")
	if err != nil {
		t.Fatalf("ensureKubeadmConfigContent() error = %v", err)
	}
	if !changed {
		t.Fatalf("ensureKubeadmConfigContent() should report changed")
	}

	header, body := splitYAMLDocumentHeader(content.Value)
	if header != "---" {
		t.Fatalf("expected YAML document header to be preserved, got %q", header)
	}

	documents := mustParseYAMLDocuments(t, body)
	if len(documents) != 3 {
		t.Fatalf("expected 3 yaml documents, got %d", len(documents))
	}

	expectScalarValue(t, documents[0], "kind", "ClusterConfiguration")
	if nodeRegistration := findYAMLMapValue(documents[0], "nodeRegistration"); nodeRegistration != nil {
		t.Fatalf("ClusterConfiguration should not gain nodeRegistration, got %#v", nodeRegistration)
	}

	expectScalarValue(t, documents[1], "kind", "InitConfiguration")
	nodeRegistration := findYAMLMapValue(documents[1], "nodeRegistration")
	if nodeRegistration == nil {
		t.Fatalf("InitConfiguration should keep nodeRegistration")
	}

	kubeletExtraArgs := findYAMLMapValue(nodeRegistration, "kubeletExtraArgs")
	if kubeletExtraArgs == nil || kubeletExtraArgs.Kind != yaml.MappingNode {
		t.Fatalf("v1beta3 kubeletExtraArgs should stay a mapping, got %#v", kubeletExtraArgs)
	}
	expectScalarValue(t, kubeletExtraArgs, "event-qps", "0")
	expectScalarValue(t, kubeletExtraArgs, "provider-id", kubeadmProviderIDValue)

	expectScalarValue(t, documents[2], "kind", "KubeProxyConfiguration")
	expectScalarValue(t, documents[2], "metricsBindAddress", "0.0.0.0:10249")
}

func TestEnsureKubeadmConfigContentSupportsV1Beta4JoinConfigurationSequence(t *testing.T) {
	content := newStringYAMLNode(`---
apiVersion: kubeadm.k8s.io/v1beta4
kind: JoinConfiguration
nodeRegistration:
  kubeletExtraArgs:
    - name: event-qps
      value: "0"
`)

	changed, err := ensureKubeadmConfigContent(content, "")
	if err != nil {
		t.Fatalf("ensureKubeadmConfigContent() error = %v", err)
	}
	if !changed {
		t.Fatalf("ensureKubeadmConfigContent() should report changed")
	}

	_, body := splitYAMLDocumentHeader(content.Value)
	documents := mustParseYAMLDocuments(t, body)
	if len(documents) != 1 {
		t.Fatalf("expected 1 yaml document, got %d", len(documents))
	}

	nodeRegistration := findYAMLMapValue(documents[0], "nodeRegistration")
	if nodeRegistration == nil {
		t.Fatalf("JoinConfiguration should keep nodeRegistration")
	}

	kubeletExtraArgs := findYAMLMapValue(nodeRegistration, "kubeletExtraArgs")
	if kubeletExtraArgs == nil || kubeletExtraArgs.Kind != yaml.SequenceNode {
		t.Fatalf("v1beta4 kubeletExtraArgs should stay a sequence, got %#v", kubeletExtraArgs)
	}

	if eventQPS := findNamedValueSequenceItem(kubeletExtraArgs, "event-qps"); eventQPS == nil {
		t.Fatalf("existing kubeletExtraArgs entry should be preserved")
	} else if value := findYAMLMapValue(eventQPS, "value"); value == nil || value.Value != "0" {
		t.Fatalf("event-qps value should be preserved, got %#v", value)
	}

	if providerID := findNamedValueSequenceItem(kubeletExtraArgs, "provider-id"); providerID == nil {
		t.Fatalf("provider-id should be added to sequence kubeletExtraArgs")
	} else if value := findYAMLMapValue(providerID, "value"); value == nil || value.Value != kubeadmProviderIDValue {
		t.Fatalf("provider-id value should be %q, got %#v", kubeadmProviderIDValue, value)
	}
}

func mustParseYAMLDocuments(t *testing.T, data string) []*yaml.Node {
	t.Helper()

	documents, ok := parseYAMLDocuments(data)
	if !ok {
		t.Fatalf("failed to parse yaml documents from:\n%s", data)
	}

	return documents
}

func expectScalarValue(t *testing.T, node *yaml.Node, key, expected string) {
	t.Helper()

	value := findYAMLMapValue(node, key)
	if value == nil {
		t.Fatalf("expected key %q to exist", key)
	}
	if value.Kind != yaml.ScalarNode || value.Value != expected {
		t.Fatalf("unexpected value for %q: got kind=%d value=%q want %q", key, value.Kind, value.Value, expected)
	}
}

func findNamedValueSequenceItem(sequence *yaml.Node, name string) *yaml.Node {
	if sequence == nil || sequence.Kind != yaml.SequenceNode {
		return nil
	}

	for _, item := range sequence.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}

		itemName := findYAMLMapValue(item, "name")
		if itemName != nil && itemName.Value == name {
			return item
		}
	}

	return nil
}

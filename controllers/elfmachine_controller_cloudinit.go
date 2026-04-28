package controllers

import (
	"bytes"
	goctx "context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	kubeadmAPIVersionV1Beta3 = "kubeadm.k8s.io/v1beta3"
	kubeadmAPIVersionV1Beta4 = "kubeadm.k8s.io/v1beta4"
	kubeadmProviderIDValue   = "elf://{{ ds.meta_data.instance_id }}"
)

type cloudInitMutationContext struct {
	hostName string
}

type cloudInitMutator func(root *yaml.Node, mutationCtx cloudInitMutationContext) (bool, error)

var cloudInitMutators = []cloudInitMutator{
	ensureHostnameCommandsInRunCmd,
	ensureKubeadmConfigInWriteFiles,
}

func patchCloudInitBootstrapData(ctx goctx.Context, bootstrapData string, mutationCtx cloudInitMutationContext) (data string, err error) {
	changed := false

	defer func() {
		if err == nil && changed {
			log := ctrl.LoggerFrom(ctx)
			log.V(3).Info("patched cloud-init bootstrap data", "hostname", mutationCtx.hostName)
		}
	}()
	header, body := splitCloudConfigHeader(bootstrapData)

	root, ok := parseYAMLMapping(body)
	if !ok {
		// Keep passthrough behavior for bootstrap formats we do not explicitly support.
		return bootstrapData, nil
	}

	for _, mutator := range cloudInitMutators {
		updated, err := mutator(root, mutationCtx)
		if err != nil {
			return "", err
		}
		if updated {
			changed = true
		}
	}

	if !changed {
		return bootstrapData, nil
	}

	marshaled, err := yaml.Marshal(root)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal bootstrap data after mutating cloud-init")
	}

	if header == "" {
		return string(marshaled), nil
	}

	return header + "\n" + string(marshaled), nil
}

func ensureHostnameCommandsInRunCmd(root *yaml.Node, mutationCtx cloudInitMutationContext) (bool, error) {
	if mutationCtx.hostName == "" {
		return false, nil
	}

	runcmd := findYAMLMapValue(root, "runcmd")
	desiredCommands := []string{
		`echo "::1         ipv6-localhost ipv6-loopback" >/etc/hosts`,
		`echo "127.0.0.1   localhost" >>/etc/hosts`,
		fmt.Sprintf(`echo "127.0.0.1   %s" >>/etc/hosts`, mutationCtx.hostName),
	}
	runcmd.Content = append(newScalarNodes(desiredCommands), runcmd.Content...)

	return true, nil
}

func ensureKubeadmConfigInWriteFiles(root *yaml.Node, mutationCtx cloudInitMutationContext) (bool, error) {
	writeFiles := findYAMLMapValue(root, "write_files")
	if writeFiles == nil || writeFiles.Kind != yaml.SequenceNode {
		return false, nil
	}

	changed := false
	for _, item := range writeFiles.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}

		path := findYAMLMapValue(item, "path")
		content := findYAMLMapValue(item, "content")
		if path == nil || content == nil || !isKubeadmConfigWriteFile(path.Value) {
			continue
		}

		updated, err := ensureKubeadmConfigContent(content, mutationCtx.hostName)
		if err != nil {
			return false, err
		}
		if updated {
			changed = true
		}
	}

	return changed, nil
}

func ensureKubeadmConfigContent(content *yaml.Node, hostname string) (bool, error) {
	if content.Kind != yaml.ScalarNode || (content.Tag != "" && content.Tag != "!!str") {
		return false, nil
	}

	header, body := splitYAMLDocumentHeader(content.Value)
	documents, ok := parseYAMLDocuments(body)
	if !ok {
		return false, nil
	}

	if !ensureKubeadmNodeRegistrationDocuments(documents, hostname) {
		return false, nil
	}

	marshaled, err := marshalYAMLDocuments(documents, header)
	if err != nil {
		return false, errors.Wrap(err, "failed to marshal kubeadm config after ensuring provider-id")
	}

	content.Tag = "!!str"
	content.Value = marshaled

	return true, nil
}

func ensureKubeadmNodeRegistrationDocuments(documents []*yaml.Node, hostname string) bool {
	changed := false
	for _, document := range documents {
		if !isKubeadmNodeRegistrationDocument(document) {
			continue
		}

		if ensureKubeadmNodeRegistration(document, hostname) {
			changed = true
		}
	}

	return changed
}

func isKubeadmNodeRegistrationDocument(root *yaml.Node) bool {
	switch getYAMLMapString(root, "kind") {
	case "InitConfiguration", "JoinConfiguration":
		return true
	default:
		return false
	}
}

func ensureKubeadmNodeRegistration(root *yaml.Node, hostname string) bool {
	changed := false
	nodeRegistration, _ := ensureYAMLMappingValue(root, "nodeRegistration")
	if ensureKubeletProviderID(root, nodeRegistration) {
		changed = true
	}

	return changed
}

func ensureKubeletProviderID(root, nodeRegistration *yaml.Node) bool {
	kubeletExtraArgs := findYAMLMapValue(nodeRegistration, "kubeletExtraArgs")
	if kubeletExtraArgs == nil {
		switch getYAMLMapString(root, "apiVersion") {
		case kubeadmAPIVersionV1Beta3:
			kubeletExtraArgs = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			appendYAMLMapValue(nodeRegistration, "kubeletExtraArgs", kubeletExtraArgs)
		case kubeadmAPIVersionV1Beta4:
			kubeletExtraArgs = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			appendYAMLMapValue(nodeRegistration, "kubeletExtraArgs", kubeletExtraArgs)
		default:
			return false
		}
	}

	switch kubeletExtraArgs.Kind {
	case yaml.MappingNode: // kubeadm.k8s.io/v1beta3
		return upsertYAMLMapString(kubeletExtraArgs, "provider-id", kubeadmProviderIDValue)
	case yaml.SequenceNode: // kubeadm.k8s.io/v1beta4
		return upsertNamedValueSequenceItem(kubeletExtraArgs, "provider-id", kubeadmProviderIDValue)
	default:
		return false
	}
}

func splitCloudConfigHeader(data string) (string, string) {
	lines := strings.Split(data, "\n")
	if len(lines) == 0 {
		return "", data
	}

	index := 0
	header := make([]string, 0, 2)
	if strings.TrimSpace(lines[index]) == "## template: jinja" {
		header = append(header, lines[index])
		index++
	}

	if index >= len(lines) || strings.TrimSpace(lines[index]) != "#cloud-config" {
		return "", data
	}

	header = append(header, lines[index])
	index++

	for index < len(lines) && strings.TrimSpace(lines[index]) == "" {
		index++
	}

	return strings.Join(header, "\n"), strings.Join(lines[index:], "\n")
}

func splitYAMLDocumentHeader(data string) (string, string) {
	lines := strings.Split(data, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", data
	}

	index := 1
	for index < len(lines) && strings.TrimSpace(lines[index]) == "" {
		index++
	}

	return lines[0], strings.Join(lines[index:], "\n")
}

func parseYAMLMapping(data string) (*yaml.Node, bool) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(data), &document); err != nil {
		return nil, false
	}

	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, false
	}

	return document.Content[0], true
}

func parseYAMLDocuments(data string) ([]*yaml.Node, bool) {
	decoder := yaml.NewDecoder(strings.NewReader(data))
	documents := make([]*yaml.Node, 0)

	for {
		var document yaml.Node
		if err := decoder.Decode(&document); err != nil {
			if err == io.EOF {
				break
			}

			return nil, false
		}

		if len(document.Content) == 0 {
			continue
		}

		documents = append(documents, document.Content[0])
	}

	return documents, len(documents) > 0
}

func marshalYAMLDocuments(documents []*yaml.Node, header string) (string, error) {
	rendered := make([]string, 0, len(documents))
	for _, document := range documents {
		marshaled, err := yaml.Marshal(document)
		if err != nil {
			return "", err
		}

		rendered = append(rendered, strings.TrimSuffix(string(marshaled), "\n"))
	}

	var buffer bytes.Buffer
	if header != "" {
		buffer.WriteString(header)
		buffer.WriteString("\n")
	}
	buffer.WriteString(strings.Join(rendered, "\n---\n"))
	buffer.WriteString("\n")

	return buffer.String(), nil
}

func isKubeadmConfigWriteFile(path string) bool {
	return strings.HasPrefix(path, "/run/kubeadm/") && strings.HasSuffix(path, ".yaml")
}

func ensureYAMLMappingValue(parent *yaml.Node, key string) (*yaml.Node, bool) {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil, false
	}

	if existing := findYAMLMapValue(parent, key); existing != nil {
		if existing.Kind != yaml.MappingNode {
			return nil, false
		}

		return existing, false
	}

	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendYAMLMapValue(parent, key, child)

	return child, true
}

func findYAMLMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

func getYAMLMapString(node *yaml.Node, key string) string {
	value := findYAMLMapValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}

	return value.Value
}

func appendYAMLMapValue(parent *yaml.Node, key string, value *yaml.Node) {
	parent.Content = append(parent.Content, newStringYAMLNode(key), value)
}

func upsertYAMLMapString(parent *yaml.Node, key, value string) bool {
	existing := findYAMLMapValue(parent, key)
	if existing == nil {
		appendYAMLMapValue(parent, key, newStringYAMLNode(value))
		return true
	}

	if existing.Kind != yaml.ScalarNode {
		return false
	}
	if existing.Value == value {
		return false
	}

	existing.Tag = "!!str"
	existing.Value = value

	return true
}

func upsertNamedValueSequenceItem(sequence *yaml.Node, name, value string) bool {
	for _, item := range sequence.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}

		itemName := findYAMLMapValue(item, "name")
		if itemName == nil || itemName.Value != name {
			continue
		}

		return upsertYAMLMapString(item, "value", value)
	}

	sequence.Content = append(sequence.Content, newNamedValueMappingNode(name, value))
	return true
}

func newBoolYAMLNode(value bool) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!bool",
		Value: strconv.FormatBool(value),
	}
}

func newScalarNodes(values []string) []*yaml.Node {
	nodes := make([]*yaml.Node, 0, len(values))
	for _, value := range values {
		nodes = append(nodes, newStringYAMLNode(value))
	}

	return nodes
}

func newNamedValueMappingNode(name, value string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendYAMLMapValue(node, "name", newStringYAMLNode(name))
	appendYAMLMapValue(node, "value", newStringYAMLNode(value))

	return node
}

func newStringYAMLNode(value string) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: value,
	}
}

package service

import (
	"context"
	"encoding/json"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/smartxworks/cluster-api-provider-elf/pkg/session"
)

type PatchStringValue struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

func NewNodeSrevice(ctx context.Context, ctlClient client.Client, cluster *clusterv1.Cluster) (*NodeSrevice, error) {
	kubeClient, err := session.NewKubeClient(ctx, ctlClient, cluster)
	if err != nil {
		return nil, err
	}

	return &NodeSrevice{kubeClient}, nil
}

type NodeSrevice struct {
	KubeClient kubernetes.Interface
}

// FindAll gets all k8s nodes resource for the given CAPI Cluster.
func (svr *NodeSrevice) FindAll() (*v1.NodeList, error) {
	listOpt := metav1.ListOptions{}
	nodeList, err := svr.KubeClient.CoreV1().Nodes().List(listOpt)
	if err != nil {
		return nil, err
	}

	return nodeList, nil
}

// SetProviderID sets the providerID for the given node.
func (svr *NodeSrevice) SetProviderID(name, providerID string) (*v1.Node, error) {
	var payloads []interface{}
	payloads = append(payloads,
		PatchStringValue{
			Op:    "add",
			Path:  "/spec/providerID",
			Value: providerID,
		})
	payloadBytes, _ := json.Marshal(payloads)

	node, err := svr.KubeClient.CoreV1().Nodes().Patch(name, types.JSONPatchType, payloadBytes)
	if err != nil {
		return nil, err
	}

	return node, nil
}

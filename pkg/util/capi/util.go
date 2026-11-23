/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package capi

import (
	"context"
	stderrors "errors"
	"fmt"

	pkgerrors "github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	capiv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Copied from https://github.com/kubernetes-sigs/cluster-api/blob/release-1.10/util/util.go.

var (
	// ErrNoCluster is returned when the cluster
	// label could not be found on the object passed in.
	ErrNoCluster = fmt.Errorf("no %q label present", capiv1.ClusterNameLabel)

	// ErrUnstructuredFieldNotFound determines that a field
	// in an unstructured object could not be found.
	ErrUnstructuredFieldNotFound = stderrors.New("field not found")
)

// GetClusterFromMetadata returns the Cluster object (if present) using the object metadata.
func GetClusterFromMetadata(ctx context.Context, c client.Client, obj metav1.ObjectMeta) (*capiv1.Cluster, error) {
	if obj.Labels[capiv1.ClusterNameLabel] == "" {
		return nil, pkgerrors.WithStack(ErrNoCluster)
	}
	return GetClusterByName(ctx, c, obj.Namespace, obj.Labels[capiv1.ClusterNameLabel])
}

// GetOwnerCluster returns the Cluster object owning the current resource.
func GetOwnerCluster(ctx context.Context, c client.Client, obj metav1.ObjectMeta) (*capiv1.Cluster, error) {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind != "Cluster" {
			continue
		}
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return nil, pkgerrors.WithStack(err)
		}
		if gv.Group == capiv1.GroupVersion.Group {
			return GetClusterByName(ctx, c, obj.Namespace, ref.Name)
		}
	}
	return nil, nil
}

// GetClusterByName finds and return a Cluster object using the specified params.
func GetClusterByName(ctx context.Context, c client.Client, namespace, name string) (*capiv1.Cluster, error) {
	cluster := &capiv1.Cluster{}
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}

	if err := c.Get(ctx, key, cluster); err != nil {
		return nil, pkgerrors.Wrapf(err, "failed to get Cluster/%s", name)
	}

	return cluster, nil
}

// GetOwnerMachine returns the Machine object owning the current resource.
func GetOwnerMachine(ctx context.Context, c client.Client, obj metav1.ObjectMeta) (*capiv1.Machine, error) {
	for _, ref := range obj.GetOwnerReferences() {
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return nil, err
		}
		if ref.Kind == "Machine" && gv.Group == capiv1.GroupVersion.Group {
			return GetMachineByName(ctx, c, obj.Namespace, ref.Name)
		}
	}
	return nil, nil
}

// GetMachineByName finds and return a Machine object using the specified params.
func GetMachineByName(ctx context.Context, c client.Client, namespace, name string) (*capiv1.Machine, error) {
	m := &capiv1.Machine{}
	key := client.ObjectKey{Name: name, Namespace: namespace}
	if err := c.Get(ctx, key, m); err != nil {
		return nil, err
	}
	return m, nil
}

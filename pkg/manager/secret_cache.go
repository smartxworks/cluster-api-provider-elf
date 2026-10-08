/*
Copyright 2026.

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

package manager

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigureSecretCaching caches all Secrets in the default namespace and
// cluster-associated Secrets in other namespaces. The manager client reads
// cached Secrets when possible and falls back to the API server for Secrets
// excluded by the cache.
func ConfigureSecretCaching(opts *Options) error {
	requirement, err := labels.NewRequirement(clusterv1.ClusterNameLabel, selection.Exists, nil)
	if err != nil {
		return err
	}

	if opts.Cache.ByObject == nil {
		opts.Cache.ByObject = map[client.Object]cache.ByObject{}
	}
	for obj := range opts.Cache.ByObject {
		if _, ok := obj.(*corev1.Secret); ok {
			delete(opts.Cache.ByObject, obj)
		}
	}
	opts.Cache.ByObject[&corev1.Secret{}] = cache.ByObject{
		Namespaces: map[string]cache.Config{
			corev1.NamespaceDefault: {
				LabelSelector: labels.Everything(),
			},
			cache.AllNamespaces: {
				LabelSelector: labels.NewSelector().Add(*requirement),
			},
		},
	}

	if opts.Client.Cache == nil {
		opts.Client.Cache = &client.CacheOptions{}
	}
	disableFor := opts.Client.Cache.DisableFor[:0]
	for _, obj := range opts.Client.Cache.DisableFor {
		if _, ok := obj.(*corev1.Secret); !ok {
			disableFor = append(disableFor, obj)
		}
	}
	opts.Client.Cache.DisableFor = disableFor

	newClient := opts.NewClient
	if newClient == nil {
		newClient = client.New
	}
	opts.NewClient = func(config *rest.Config, clientOptions client.Options) (client.Client, error) {
		cachedClient, err := newClient(config, clientOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to create cache-backed client: %w", err)
		}

		uncachedOptions := clientOptions
		uncachedOptions.Cache = nil
		uncachedClient, err := newClient(config, uncachedOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to create API client: %w", err)
		}

		return &secretRoutingClient{
			Client:    cachedClient,
			apiReader: uncachedClient,
		}, nil
	}

	return nil
}

type secretRoutingClient struct {
	client.Client
	apiReader client.Reader
}

func (c *secretRoutingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); !ok || key.Namespace == corev1.NamespaceDefault {
		return c.Client.Get(ctx, key, obj, opts...)
	}

	if err := c.Client.Get(ctx, key, obj, opts...); !apierrors.IsNotFound(err) {
		return err
	}

	return c.apiReader.Get(ctx, key, obj, opts...)
}

func (c *secretRoutingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*corev1.SecretList); !ok {
		return c.Client.List(ctx, list, opts...)
	}

	listOptions := (&client.ListOptions{}).ApplyOptions(opts)
	if listOptions.Namespace == corev1.NamespaceDefault || requiresClusterNameLabel(listOptions.LabelSelector) {
		return c.Client.List(ctx, list, opts...)
	}

	return c.apiReader.List(ctx, list, opts...)
}

func requiresClusterNameLabel(selector labels.Selector) bool {
	if selector == nil {
		return false
	}

	requirements, selectable := selector.Requirements()
	if !selectable {
		return false
	}
	for _, requirement := range requirements {
		if requirement.Key() != clusterv1.ClusterNameLabel {
			continue
		}

		switch requirement.Operator() {
		case selection.Equals, selection.DoubleEquals, selection.In, selection.Exists, selection.GreaterThan, selection.LessThan:
			return true
		}
	}

	return false
}

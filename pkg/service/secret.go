package service

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewSecretService(client client.Client) *SecretService {
	return &SecretService{client}
}

type SecretService struct {
	Client client.Client
}

func (svr *SecretService) GetBootstrapData(ctx context.Context, namespace, name string) (string, error) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}

	if err := svr.Client.Get(ctx, key, secret); err != nil {
		return "", errors.Wrapf(err, "failed to retrieve bootstrap data secret for %s %s", key.Namespace, key.Name)
	}

	value, ok := secret.Data["value"]
	if !ok {
		return "", errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return string(value), nil
}

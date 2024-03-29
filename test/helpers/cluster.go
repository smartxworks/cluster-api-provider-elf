package helpers

import (
	goctx "context"
	"os"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capisecret "sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateKubeConfigSecret uses kubeconfig of testEnv to create the workload cluster kubeconfig secret.
func CreateKubeConfigSecret(testEnv *TestEnvironment, namespace, clusterName string) error {
	// Return if the secret already exists
	if s, err := GetKubeConfigSecret(testEnv, namespace, clusterName); err != nil || s != nil {
		return err
	}

	secret, err := NewKubeConfigSecret(testEnv, namespace, clusterName)
	if err != nil {
		return err
	}

	return testEnv.CreateAndWait(goctx.Background(), secret)
}

// GetKubeConfigSecret uses kubeconfig of testEnv to get the workload cluster kubeconfig secret.
func GetKubeConfigSecret(testEnv *TestEnvironment, namespace, clusterName string) (*corev1.Secret, error) {
	var secret corev1.Secret
	secretKey := client.ObjectKey{
		Namespace: namespace,
		Name:      capisecret.Name(clusterName, capisecret.Kubeconfig),
	}
	if err := testEnv.Get(goctx.Background(), secretKey, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to get kubeconfig secret %s/%s", secretKey.Namespace, secretKey.Name)
	}
	return &secret, nil
}

// DeleteKubeConfigSecret delete the workload cluster kubeconfig secret.
func DeleteKubeConfigSecret(testEnv *TestEnvironment, namespace, clusterName string) error {
	deleteSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      capisecret.Name(clusterName, capisecret.Kubeconfig),
		},
	}

	if err := testEnv.Delete(goctx.Background(), deleteSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, "failed to delete kubeconfig secret %s/%s", deleteSecret.Namespace, deleteSecret.Name)
	}
	return nil
}

// NewKubeConfigSecret uses kubeconfig of testEnv to generate the workload cluster kubeconfig secret.
func NewKubeConfigSecret(testEnv *TestEnvironment, namespace, clusterName string) (*corev1.Secret, error) {
	bs, err := os.ReadFile(testEnv.Kubeconfig)
	if err != nil {
		return nil, err
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      capisecret.Name(clusterName, capisecret.Kubeconfig),
			Namespace: namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			},
		},
		Data: map[string][]byte{
			capisecret.KubeconfigDataName: bs,
		},
		Type: clusterv1.ClusterSecretType,
	}, nil
}

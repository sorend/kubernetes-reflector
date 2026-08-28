package mirror

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type SecretOperations struct {
	client kubernetes.Interface
}

func NewSecretOperations(client kubernetes.Interface) *SecretOperations {
	return &SecretOperations{client: client}
}

func (s *SecretOperations) ListAllWithName(ctx context.Context, name string) ([]metav1.Object, error) {
	list, err := s.client.CoreV1().Secrets("").List(ctx, metav1.ListOptions{FieldSelector: "metadata.name=" + name})
	if err != nil {
		return nil, err
	}

	result := make([]metav1.Object, 0, len(list.Items))
	for i := range list.Items {
		result = append(result, &list.Items[i])
	}
	return result, nil
}

func (s *SecretOperations) Get(ctx context.Context, ns, name string) (metav1.Object, error) {
	return s.client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
}

func (s *SecretOperations) Create(ctx context.Context, obj metav1.Object, ns string) error {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return ErrUnexpectedObjectType("secret create", obj)
	}
	_, err := s.client.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	return err
}

func (s *SecretOperations) Patch(ctx context.Context, ns, name string, patchData []byte) error {
	_, err := s.client.CoreV1().Secrets(ns).Patch(ctx, name, types.JSONPatchType, patchData, metav1.PatchOptions{})
	return err
}

func (s *SecretOperations) Delete(ctx context.Context, ns, name string) error {
	return s.client.CoreV1().Secrets(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

func (s *SecretOperations) Clone(src metav1.Object) (metav1.Object, error) {
	secret, ok := src.(*corev1.Secret)
	if !ok {
		return nil, ErrUnexpectedObjectType("secret clone", src)
	}
	return &corev1.Secret{Type: secret.Type, Data: cloneByteMap(secret.Data)}, nil
}

func (s *SecretOperations) DataPatchOps(src metav1.Object) []map[string]interface{} {
	secret, ok := src.(*corev1.Secret)
	if !ok {
		return nil
	}
	return []map[string]interface{}{{
		"op":    "replace",
		"path":  "/data",
		"value": secret.Data,
	}}
}

func (s *SecretOperations) ResourceType() string {
	return "Secret"
}

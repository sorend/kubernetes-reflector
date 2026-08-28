package mirror

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type ConfigMapOperations struct {
	client kubernetes.Interface
}

func NewConfigMapOperations(client kubernetes.Interface) *ConfigMapOperations {
	return &ConfigMapOperations{client: client}
}

func (c *ConfigMapOperations) ListAllWithName(ctx context.Context, name string) ([]metav1.Object, error) {
	list, err := c.client.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{FieldSelector: "metadata.name=" + name})
	if err != nil {
		return nil, err
	}

	result := make([]metav1.Object, 0, len(list.Items))
	for i := range list.Items {
		result = append(result, &list.Items[i])
	}
	return result, nil
}

func (c *ConfigMapOperations) Get(ctx context.Context, ns, name string) (metav1.Object, error) {
	return c.client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
}

func (c *ConfigMapOperations) Create(ctx context.Context, obj metav1.Object, ns string) error {
	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return ErrUnexpectedObjectType("configmap create", obj)
	}
	_, err := c.client.CoreV1().ConfigMaps(ns).Create(ctx, configMap, metav1.CreateOptions{})
	return err
}

func (c *ConfigMapOperations) Patch(ctx context.Context, ns, name string, patchData []byte) error {
	_, err := c.client.CoreV1().ConfigMaps(ns).Patch(ctx, name, types.JSONPatchType, patchData, metav1.PatchOptions{})
	return err
}

func (c *ConfigMapOperations) Delete(ctx context.Context, ns, name string) error {
	return c.client.CoreV1().ConfigMaps(ns).Delete(ctx, name, metav1.DeleteOptions{})
}

func (c *ConfigMapOperations) Clone(src metav1.Object) (metav1.Object, error) {
	configMap, ok := src.(*corev1.ConfigMap)
	if !ok {
		return nil, ErrUnexpectedObjectType("configmap clone", src)
	}
	return &corev1.ConfigMap{Data: cloneStringMap(configMap.Data), BinaryData: cloneByteMap(configMap.BinaryData)}, nil
}

func (c *ConfigMapOperations) DataPatchOps(src metav1.Object) []map[string]interface{} {
	configMap, ok := src.(*corev1.ConfigMap)
	if !ok {
		return nil
	}
	return []map[string]interface{}{
		{
			"op":    "replace",
			"path":  "/data",
			"value": configMap.Data,
		},
		{
			"op":    "replace",
			"path":  "/binaryData",
			"value": configMap.BinaryData,
		},
	}
}

func (c *ConfigMapOperations) ResourceType() string {
	return "ConfigMap"
}

package mirror

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type ConfigMapOps struct{}

func (ConfigMapOps) NewObject() *corev1.ConfigMap { return &corev1.ConfigMap{} }
func (ConfigMapOps) NewList() client.ObjectList   { return &corev1.ConfigMapList{} }
func (ConfigMapOps) ListItems(list client.ObjectList) []*corev1.ConfigMap {
	l := list.(*corev1.ConfigMapList)
	result := make([]*corev1.ConfigMap, len(l.Items))
	for i := range l.Items {
		result[i] = &l.Items[i]
	}
	return result
}
func (ConfigMapOps) Clone(src *corev1.ConfigMap) *corev1.ConfigMap {
	data := make(map[string]string, len(src.Data))
	for k, v := range src.Data {
		data[k] = v
	}
	bin := make(map[string][]byte, len(src.BinaryData))
	for k, v := range src.BinaryData {
		bin[k] = append([]byte(nil), v...)
	}
	return &corev1.ConfigMap{Data: data, BinaryData: bin}
}
func (ConfigMapOps) CopyData(src, dst *corev1.ConfigMap) {
	data := make(map[string]string, len(src.Data))
	for k, v := range src.Data {
		data[k] = v
	}
	bin := make(map[string][]byte, len(src.BinaryData))
	for k, v := range src.BinaryData {
		bin[k] = append([]byte(nil), v...)
	}
	dst.Data = data
	dst.BinaryData = bin
}
func (ConfigMapOps) TypeName() string                  { return "ConfigMap" }
func (ConfigMapOps) Predicates() []predicate.Predicate { return nil }

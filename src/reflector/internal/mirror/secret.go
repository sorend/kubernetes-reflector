package mirror

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type SecretOps struct{}

func (SecretOps) NewObject() *corev1.Secret  { return &corev1.Secret{} }
func (SecretOps) NewList() client.ObjectList { return &corev1.SecretList{} }
func (SecretOps) ListItems(list client.ObjectList) []*corev1.Secret {
	l := list.(*corev1.SecretList)
	result := make([]*corev1.Secret, len(l.Items))
	for i := range l.Items {
		result[i] = &l.Items[i]
	}
	return result
}
func (SecretOps) Clone(src *corev1.Secret) *corev1.Secret {
	data := make(map[string][]byte, len(src.Data))
	for k, v := range src.Data {
		data[k] = append([]byte(nil), v...)
	}
	return &corev1.Secret{Type: src.Type, Data: data}
}
func (SecretOps) CopyData(src, dst *corev1.Secret) {
	data := make(map[string][]byte, len(src.Data))
	for k, v := range src.Data {
		data[k] = append([]byte(nil), v...)
	}
	dst.Data = data
}
func (SecretOps) TypeName() string { return "Secret" }
func (SecretOps) Predicates() []predicate.Predicate {
	return []predicate.Predicate{
		predicate.NewPredicateFuncs(func(obj client.Object) bool {
			s, ok := obj.(*corev1.Secret)
			return ok && !strings.HasPrefix(string(s.Type), "helm.sh")
		}),
	}
}

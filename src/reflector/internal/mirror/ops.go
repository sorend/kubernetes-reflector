package mirror

import (
	"strconv"
	"time"

	"github.com/sorend/kubernetes-reflector/internal/annotations"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type ResourceOps[T client.Object] interface {
	NewObject() T
	NewList() client.ObjectList
	ListItems(client.ObjectList) []T
	Clone(src T) T
	CopyData(src, dst T)
	TypeName() string
	Predicates() []predicate.Predicate
}

func buildReflectionAnnotations(existing map[string]string, src client.Object, auto bool) map[string]string {
	out := make(map[string]string, len(existing)+4)
	for k, v := range existing {
		out[k] = v
	}
	out[annotations.Reflects] = src.GetNamespace() + "/" + src.GetName()
	out[annotations.MetaAutoReflects] = strconv.FormatBool(auto)
	out[annotations.MetaReflectedVersion] = src.GetResourceVersion()
	out[annotations.MetaReflectedAt] = time.Now().UTC().Format(time.RFC3339)
	return out
}

func cloneByteMap(source map[string][]byte) map[string][]byte {
	if source == nil {
		return nil
	}
	cloned := make(map[string][]byte, len(source))
	for key, value := range source {
		if value == nil {
			cloned[key] = nil
			continue
		}
		copied := make([]byte, len(value))
		copy(copied, value)
		cloned[key] = copied
	}
	return cloned
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

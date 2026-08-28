package mirror

import (
	"context"

	"github.com/emberstack/kubernetes-reflector/internal/annotations"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	IndexReflectsSource = "reflectsSource"
	IndexIsAutoSource   = "isAutoSource"
)

func SetupIndexers(ctx context.Context, mgr manager.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Secret{}, IndexReflectsSource,
		func(obj client.Object) []string {
			s := obj.(*corev1.Secret)
			if v, ok := s.Annotations[annotations.Reflects]; ok && v != "" {
				return []string{v}
			}
			return nil
		}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Secret{}, IndexIsAutoSource,
		func(obj client.Object) []string {
			s := obj.(*corev1.Secret)
			p := GetMirroringProperties(s.Annotations, s.ResourceVersion)
			if p.Allowed && p.AutoEnabled {
				return []string{"true"}
			}
			return nil
		}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.ConfigMap{}, IndexReflectsSource,
		func(obj client.Object) []string {
			cm := obj.(*corev1.ConfigMap)
			if v, ok := cm.Annotations[annotations.Reflects]; ok && v != "" {
				return []string{v}
			}
			return nil
		}); err != nil {
		return err
	}

	return mgr.GetFieldIndexer().IndexField(ctx, &corev1.ConfigMap{}, IndexIsAutoSource,
		func(obj client.Object) []string {
			cm := obj.(*corev1.ConfigMap)
			p := GetMirroringProperties(cm.Annotations, cm.ResourceVersion)
			if p.Allowed && p.AutoEnabled {
				return []string{"true"}
			}
			return nil
		})
}

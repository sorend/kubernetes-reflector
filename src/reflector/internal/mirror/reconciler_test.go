package mirror_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sorend/kubernetes-reflector/internal/annotations"
	"github.com/sorend/kubernetes-reflector/internal/config"
	"github.com/sorend/kubernetes-reflector/internal/mirror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func buildFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithIndex(&corev1.Secret{}, mirror.IndexReflectsSource, func(obj client.Object) []string {
			s := obj.(*corev1.Secret)
			if v := s.Annotations[annotations.Reflects]; v != "" {
				return []string{v}
			}
			return nil
		}).
		WithIndex(&corev1.Secret{}, mirror.IndexIsAutoSource, func(obj client.Object) []string {
			s := obj.(*corev1.Secret)
			p := mirror.GetMirroringProperties(s.Annotations, s.ResourceVersion)
			if p.Allowed && p.AutoEnabled {
				return []string{"true"}
			}
			return nil
		}).
		WithIndex(&corev1.ConfigMap{}, mirror.IndexReflectsSource, func(obj client.Object) []string {
			cm := obj.(*corev1.ConfigMap)
			if v := cm.Annotations[annotations.Reflects]; v != "" {
				return []string{v}
			}
			return nil
		}).
		WithIndex(&corev1.ConfigMap{}, mirror.IndexIsAutoSource, func(obj client.Object) []string {
			cm := obj.(*corev1.ConfigMap)
			p := mirror.GetMirroringProperties(cm.Annotations, cm.ResourceVersion)
			if p.Allowed && p.AutoEnabled {
				return []string{"true"}
			}
			return nil
		})

	return builder.Build()
}

func newSecretReconciler(c client.Client, cfg ...config.Config) *mirror.ResourceReconciler[*corev1.Secret] {
	resolved := config.Config{}
	if len(cfg) > 0 {
		resolved = cfg[0]
	}
	return mirror.NewResourceReconciler(c, mirror.SecretOps{}, resolved, zap.New())
}

func newConfigMapReconciler(c client.Client, cfg ...config.Config) *mirror.ResourceReconciler[*corev1.ConfigMap] {
	resolved := config.Config{}
	if len(cfg) > 0 {
		resolved = cfg[0]
	}
	return mirror.NewResourceReconciler(c, mirror.ConfigMapOps{}, resolved, zap.New())
}

func doReconcile[T client.Object](r *mirror.ResourceReconciler[T], ns, name string) (reconcile.Result, error) {
	return r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: name},
	})
}

var _ = Describe("ResourceReconciler (Secret)", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Source reconciliation", func() {
		It("does nothing when reflection is not allowed", func() {
			ns1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-a"}}
			src := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "default",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.ReflectionAllowed:     "false",
						annotations.ReflectionAutoEnabled: "true",
					},
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			c := buildFakeClient(ns1, src)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "default", "my-secret")
			Expect(err).NotTo(HaveOccurred())

			reflections := &corev1.SecretList{}
			Expect(c.List(ctx, reflections, client.InNamespace("ns-a"))).To(Succeed())
			Expect(reflections.Items).To(BeEmpty())
		})

		It("creates auto-reflections in eligible namespaces", func() {
			ns1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "allowed-ns"}}
			ns2 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "not-allowed-ns"}}
			src := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "default",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.ReflectionAllowed:        "true",
						annotations.ReflectionAutoEnabled:    "true",
						annotations.ReflectionAutoNamespaces: "allowed-.*",
					},
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			c := buildFakeClient(ns1, ns2, src)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "default", "my-secret")
			Expect(err).NotTo(HaveOccurred())

			reflected := &corev1.Secret{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "allowed-ns", Name: "my-secret"}, reflected)).To(Succeed())
			Expect(reflected.Annotations[annotations.Reflects]).To(Equal("default/my-secret"))
			Expect(reflected.Annotations[annotations.MetaAutoReflects]).To(Equal("true"))
			Expect(reflected.Data["key"]).To(Equal([]byte("value")))

			notReflected := &corev1.Secret{}
			err = c.Get(ctx, types.NamespacedName{Namespace: "not-allowed-ns", Name: "my-secret"}, notReflected)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("skips excluded namespaces for auto-reflections", func() {
			allowed := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "allowed-ns"}}
			excluded := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "skip-me"}}
			src := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "default",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.ReflectionAllowed:     "true",
						annotations.ReflectionAutoEnabled: "true",
					},
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			c := buildFakeClient(allowed, excluded, src)
			r := newSecretReconciler(c, config.Config{ExcludedNamespaces: "skip-*"})

			_, err := doReconcile(r, "default", "my-secret")
			Expect(err).NotTo(HaveOccurred())

			Expect(c.Get(ctx, types.NamespacedName{Namespace: "allowed-ns", Name: "my-secret"}, &corev1.Secret{})).To(Succeed())
			err = c.Get(ctx, types.NamespacedName{Namespace: "skip-me", Name: "my-secret"}, &corev1.Secret{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("skips helm secrets entirely", func() {
			ns1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "allowed-ns"}}
			src := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "default",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.ReflectionAllowed:     "true",
						annotations.ReflectionAutoEnabled: "true",
					},
				},
				Type: corev1.SecretType("helm.sh/release.v1"),
				Data: map[string][]byte{"key": []byte("value")},
			}
			c := buildFakeClient(ns1, src)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "default", "my-secret")
			Expect(err).NotTo(HaveOccurred())
			err = c.Get(ctx, types.NamespacedName{Namespace: "allowed-ns", Name: "my-secret"}, &corev1.Secret{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("deletes stale auto-reflections when a namespace is no longer eligible", func() {
			ns1 := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "my-ns"}}
			src := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "default",
					ResourceVersion: "2",
					Annotations: map[string]string{
						annotations.ReflectionAllowed:        "true",
						annotations.ReflectionAutoEnabled:    "true",
						annotations.ReflectionAutoNamespaces: "no-match-.*",
					},
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			stale := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "my-ns",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.Reflects:             "default/my-secret",
						annotations.MetaAutoReflects:     "true",
						annotations.MetaReflectedVersion: "1",
					},
				},
			}
			c := buildFakeClient(ns1, src, stale)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "default", "my-secret")
			Expect(err).NotTo(HaveOccurred())

			err = c.Get(ctx, types.NamespacedName{Namespace: "my-ns", Name: "my-secret"}, &corev1.Secret{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("updates direct reflections when the source version changes", func() {
			src := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "default",
					ResourceVersion: "2",
					Annotations: map[string]string{
						annotations.ReflectionAllowed: "true",
					},
				},
				Data: map[string][]byte{"key": []byte("new-value")},
			}
			targetNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target-ns"}}
			directRefl := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "target-ns",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.Reflects:             "default/my-secret",
						annotations.MetaAutoReflects:     "false",
						annotations.MetaReflectedVersion: "1",
					},
				},
				Data: map[string][]byte{"key": []byte("old-value")},
			}
			c := buildFakeClient(src, targetNs, directRefl)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "default", "my-secret")
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "target-ns", Name: "my-secret"}, updated)).To(Succeed())
			Expect(updated.Data["key"]).To(Equal([]byte("new-value")))
			Expect(updated.Annotations[annotations.MetaAutoReflects]).To(Equal("false"))
		})

		It("deletes all auto-reflections when the source is deleted", func() {
			autoRefl := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "ns-a",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.Reflects:         "default/my-secret",
						annotations.MetaAutoReflects: "true",
					},
				},
			}
			directRefl := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "ns-b",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.Reflects:         "default/my-secret",
						annotations.MetaAutoReflects: "false",
					},
				},
			}
			c := buildFakeClient(autoRefl, directRefl)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "default", "my-secret")
			Expect(err).NotTo(HaveOccurred())

			err = c.Get(ctx, types.NamespacedName{Namespace: "ns-a", Name: "my-secret"}, &corev1.Secret{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "ns-b", Name: "my-secret"}, &corev1.Secret{})).To(Succeed())
		})
	})

	Describe("Auto-reflection reconciliation", func() {
		It("syncs data when the source version changes", func() {
			src := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "default",
					ResourceVersion: "2",
					Annotations: map[string]string{
						annotations.ReflectionAllowed:     "true",
						annotations.ReflectionAutoEnabled: "true",
					},
				},
				Data: map[string][]byte{"key": []byte("new-value")},
			}
			targetNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target-ns"}}
			autoRefl := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "target-ns",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.Reflects:             "default/my-secret",
						annotations.MetaAutoReflects:     "true",
						annotations.MetaReflectedVersion: "1",
					},
				},
				Data: map[string][]byte{"key": []byte("old-value")},
			}
			c := buildFakeClient(src, targetNs, autoRefl)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "target-ns", "my-secret")
			Expect(err).NotTo(HaveOccurred())

			updated := &corev1.Secret{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "target-ns", Name: "my-secret"}, updated)).To(Succeed())
			Expect(updated.Data["key"]).To(Equal([]byte("new-value")))
		})

		It("deletes itself when the source is gone", func() {
			targetNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "target-ns"}}
			autoRefl := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "target-ns",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.Reflects:         "default/my-secret",
						annotations.MetaAutoReflects: "true",
					},
				},
			}
			c := buildFakeClient(targetNs, autoRefl)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "target-ns", "my-secret")
			Expect(err).NotTo(HaveOccurred())

			err = c.Get(ctx, types.NamespacedName{Namespace: "target-ns", Name: "my-secret"}, &corev1.Secret{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("Direct reflection reconciliation", func() {
		It("syncs data from the source", func() {
			src := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "src",
					Namespace:       "default",
					ResourceVersion: "2",
					Annotations:     map[string]string{annotations.ReflectionAllowed: "true"},
				},
				Data: map[string][]byte{"k": []byte("v2")},
			}
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}
			refl := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "src",
					Namespace:       "other",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.Reflects:             "default/src",
						annotations.MetaReflectedVersion: "1",
					},
				},
				Data: map[string][]byte{"k": []byte("v1")},
			}
			c := buildFakeClient(src, ns, refl)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "other", "src")
			Expect(err).NotTo(HaveOccurred())

			got := &corev1.Secret{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "other", Name: "src"}, got)).To(Succeed())
			Expect(got.Data["k"]).To(Equal([]byte("v2")))
		})

		It("leaves direct reflections unchanged when the source is missing", func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}
			refl := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "src",
					Namespace:       "other",
					ResourceVersion: "1",
					Annotations: map[string]string{
						annotations.Reflects:             "default/src",
						annotations.MetaReflectedVersion: "1",
					},
				},
				Data: map[string][]byte{"k": []byte("original")},
			}
			c := buildFakeClient(ns, refl)
			r := newSecretReconciler(c)

			_, err := doReconcile(r, "other", "src")
			Expect(err).NotTo(HaveOccurred())

			got := &corev1.Secret{}
			Expect(c.Get(ctx, types.NamespacedName{Namespace: "other", Name: "src"}, got)).To(Succeed())
			Expect(got.Data["k"]).To(Equal([]byte("original")))
		})
	})
})

var _ = Describe("ResourceReconciler (ConfigMap)", func() {
	It("syncs configmap data through the generic reconciler", func() {
		ctx := context.Background()
		src := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "app-config",
				Namespace:       "default",
				ResourceVersion: "2",
				Annotations:     map[string]string{annotations.ReflectionAllowed: "true"},
			},
			Data:       map[string]string{"key": "new-value"},
			BinaryData: map[string][]byte{"bin": []byte("new-bin")},
		}
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}
		refl := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "app-config",
				Namespace:       "other",
				ResourceVersion: "1",
				Annotations: map[string]string{
					annotations.Reflects:             "default/app-config",
					annotations.MetaReflectedVersion: "1",
				},
			},
			Data:       map[string]string{"key": "old-value"},
			BinaryData: map[string][]byte{"bin": []byte("old-bin")},
		}

		c := buildFakeClient(src, ns, refl)
		r := newConfigMapReconciler(c)

		_, err := doReconcile(r, "other", "app-config")
		Expect(err).NotTo(HaveOccurred())

		got := &corev1.ConfigMap{}
		Expect(c.Get(ctx, types.NamespacedName{Namespace: "other", Name: "app-config"}, got)).To(Succeed())
		Expect(got.Data).To(Equal(map[string]string{"key": "new-value"}))
		Expect(got.BinaryData["bin"]).To(Equal([]byte("new-bin")))
	})
})

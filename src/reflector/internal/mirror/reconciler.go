package mirror

import (
	"context"
	"reflect"
	"regexp"
	"strings"

	"github.com/sorend/kubernetes-reflector/internal/config"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ResourceReconciler[T client.Object] struct {
	client.Client
	ops      ResourceOps[T]
	cfg      config.Config
	log      logr.Logger
	excluded []*regexp.Regexp
}

func NewResourceReconciler[T client.Object](c client.Client, ops ResourceOps[T], cfg config.Config, log logr.Logger) *ResourceReconciler[T] {
	return &ResourceReconciler[T]{
		Client:   c,
		ops:      ops,
		cfg:      cfg,
		log:      log,
		excluded: ParseGlobPatterns(strings.ToLower(cfg.ExcludedNamespaces)),
	}
}

func (r *ResourceReconciler[T]) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("namespace", req.Namespace, "name", req.Name, "type", r.ops.TypeName())
	if req.Namespace != "" && IsExcluded(strings.ToLower(req.Namespace), r.excluded) {
		return reconcile.Result{}, nil
	}

	obj := r.ops.NewObject()
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, r.reconcileSourceDeleted(ctx, req.NamespacedName, log)
		}
		return reconcile.Result{}, err
	}

	if secret, ok := any(obj).(*corev1.Secret); ok && strings.HasPrefix(string(secret.Type), "helm.sh") {
		return reconcile.Result{}, nil
	}

	props := GetMirroringProperties(obj.GetAnnotations(), obj.GetResourceVersion())

	switch {
	case !props.IsReflection():
		return r.reconcileSource(ctx, obj, props, log)
	case props.IsAutoReflection:
		return r.reconcileAutoReflection(ctx, obj, props, log)
	default:
		return r.reconcileDirectReflection(ctx, obj, props, log)
	}
}

func (r *ResourceReconciler[T]) reconcileSourceDeleted(ctx context.Context, sourceKey types.NamespacedName, log logr.Logger) error {
	list := r.ops.NewList()
	if err := r.List(ctx, list, client.MatchingFields{IndexReflectsSource: sourceKey.String()}); err != nil {
		return err
	}

	for _, item := range r.ops.ListItems(list) {
		p := GetMirroringProperties(item.GetAnnotations(), item.GetResourceVersion())
		if !p.IsAutoReflection {
			continue
		}
		log.Info("deleting auto-reflection because source was deleted", "reflection", item.GetNamespace()+"/"+item.GetName())
		if err := client.IgnoreNotFound(r.Delete(ctx, item)); err != nil {
			return err
		}
	}

	return nil
}

func (r *ResourceReconciler[T]) reconcileSource(ctx context.Context, src T, props MirroringProperties, log logr.Logger) (reconcile.Result, error) {
	list := r.ops.NewList()
	sourceKey := src.GetNamespace() + "/" + src.GetName()
	if err := r.List(ctx, list, client.MatchingFields{IndexReflectsSource: sourceKey}); err != nil {
		return reconcile.Result{}, err
	}

	reflections := r.ops.ListItems(list)
	autoManagedByNamespace := make(map[string]T, len(reflections))
	for _, refl := range reflections {
		if refl.GetName() == src.GetName() && refl.GetNamespace() != src.GetNamespace() {
			autoManagedByNamespace[refl.GetNamespace()] = refl
		}
	}

	if !props.Allowed {
		for _, refl := range reflections {
			rp := GetMirroringProperties(refl.GetAnnotations(), refl.GetResourceVersion())
			if !rp.IsAutoReflection {
				continue
			}
			if err := client.IgnoreNotFound(r.Delete(ctx, refl)); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	if props.AutoEnabled {
		nsList := &corev1.NamespaceList{}
		if err := r.List(ctx, nsList); err != nil {
			return reconcile.Result{}, err
		}

		validNamespaces := make(map[string]struct{}, len(nsList.Items))
		for i := range nsList.Items {
			ns := &nsList.Items[i]
			if ns.Name == src.GetNamespace() {
				continue
			}
			if IsExcluded(strings.ToLower(ns.Name), r.excluded) {
				continue
			}

			eligible := props.CanBeAutoReflectedToNamespace(ns)
			existing, exists := autoManagedByNamespace[ns.Name]

			if eligible {
				validNamespaces[ns.Name] = struct{}{}
				if !exists {
					log.Info("creating auto-reflection", "target", ns.Name+"/"+src.GetName())
					if err := r.applyReflection(ctx, src, ns.Name, src.GetName(), true); err != nil {
						log.Error(err, "failed to create auto-reflection", "target", ns.Name)
					}
					continue
				}

				ep := GetMirroringProperties(existing.GetAnnotations(), existing.GetResourceVersion())
				if ep.ReflectedVersion != src.GetResourceVersion() {
					log.Info("updating auto-reflection", "target", ns.Name+"/"+src.GetName())
					if err := r.patchReflection(ctx, src, existing, true); err != nil {
						log.Error(err, "failed to update auto-reflection", "target", ns.Name)
					}
				}
				continue
			}

			if !exists {
				continue
			}

			ep := GetMirroringProperties(existing.GetAnnotations(), existing.GetResourceVersion())
			if ep.IsAutoReflection {
				log.Info("deleting stale auto-reflection", "target", ns.Name+"/"+src.GetName())
				if err := client.IgnoreNotFound(r.Delete(ctx, existing)); err != nil {
					return reconcile.Result{}, err
				}
			}
		}

		for _, refl := range reflections {
			rp := GetMirroringProperties(refl.GetAnnotations(), refl.GetResourceVersion())
			if !rp.IsAutoReflection {
				continue
			}
			if _, ok := validNamespaces[refl.GetNamespace()]; ok {
				continue
			}
			if refl.GetNamespace() == src.GetNamespace() {
				continue
			}
			log.Info("deleting auto-reflection in gone namespace", "target", refl.GetNamespace()+"/"+refl.GetName())
			if err := client.IgnoreNotFound(r.Delete(ctx, refl)); err != nil {
				return reconcile.Result{}, err
			}
		}
	} else {
		for _, refl := range reflections {
			rp := GetMirroringProperties(refl.GetAnnotations(), refl.GetResourceVersion())
			if !rp.IsAutoReflection {
				continue
			}
			if err := client.IgnoreNotFound(r.Delete(ctx, refl)); err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	for _, refl := range reflections {
		if refl.GetNamespace() == src.GetNamespace() {
			continue
		}

		rp := GetMirroringProperties(refl.GetAnnotations(), refl.GetResourceVersion())
		if rp.IsAutoReflection || rp.ReflectedVersion == src.GetResourceVersion() {
			continue
		}

		ns := &corev1.Namespace{}
		if err := r.Get(ctx, types.NamespacedName{Name: refl.GetNamespace()}, ns); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return reconcile.Result{}, err
		}

		if !props.CanBeReflectedToNamespace(ns) {
			log.Info("source does not allow reflection to namespace", "reflection", refl.GetNamespace()+"/"+refl.GetName())
			continue
		}

		log.Info("updating direct reflection", "target", refl.GetNamespace()+"/"+refl.GetName())
		if err := r.patchReflection(ctx, src, refl, false); err != nil {
			log.Error(err, "failed to update direct reflection", "target", refl.GetNamespace())
		}
	}

	log.Info("source reconciled", "autoEnabled", props.AutoEnabled, "allowed", props.Allowed)
	return reconcile.Result{}, nil
}

func (r *ResourceReconciler[T]) reconcileAutoReflection(ctx context.Context, obj T, props MirroringProperties, log logr.Logger) (reconcile.Result, error) {
	src := r.ops.NewObject()
	if err := r.Get(ctx, types.NamespacedName{Namespace: props.Reflects.Namespace, Name: props.Reflects.Name}, src); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("source not found, deleting auto-reflection")
			return reconcile.Result{}, client.IgnoreNotFound(r.Delete(ctx, obj))
		}
		return reconcile.Result{}, err
	}

	if secret, ok := any(src).(*corev1.Secret); ok && strings.HasPrefix(string(secret.Type), "helm.sh") {
		log.Info("source is a skipped helm secret, deleting auto-reflection")
		return reconcile.Result{}, client.IgnoreNotFound(r.Delete(ctx, obj))
	}

	srcProps := GetMirroringProperties(src.GetAnnotations(), src.GetResourceVersion())
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: obj.GetNamespace()}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, client.IgnoreNotFound(r.Delete(ctx, obj))
		}
		return reconcile.Result{}, err
	}

	if IsExcluded(strings.ToLower(ns.Name), r.excluded) || !srcProps.CanBeAutoReflectedToNamespace(ns) {
		log.Info("source no longer allows auto-reflection, deleting")
		return reconcile.Result{}, client.IgnoreNotFound(r.Delete(ctx, obj))
	}

	if props.ReflectedVersion == src.GetResourceVersion() {
		return reconcile.Result{}, nil
	}

	log.Info("syncing auto-reflection from source")
	return reconcile.Result{}, r.patchReflection(ctx, src, obj, true)
}

func (r *ResourceReconciler[T]) reconcileDirectReflection(ctx context.Context, obj T, props MirroringProperties, log logr.Logger) (reconcile.Result, error) {
	src := r.ops.NewObject()
	if err := r.Get(ctx, types.NamespacedName{Namespace: props.Reflects.Namespace, Name: props.Reflects.Name}, src); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("source not found, leaving direct reflection unchanged")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if secret, ok := any(src).(*corev1.Secret); ok && strings.HasPrefix(string(secret.Type), "helm.sh") {
		log.Info("source is a skipped helm secret, leaving direct reflection unchanged")
		return reconcile.Result{}, nil
	}

	srcProps := GetMirroringProperties(src.GetAnnotations(), src.GetResourceVersion())
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: obj.GetNamespace()}, ns); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if !srcProps.CanBeReflectedToNamespace(ns) {
		log.Info("source does not permit reflection to this namespace")
		return reconcile.Result{}, nil
	}

	if props.ReflectedVersion == src.GetResourceVersion() {
		return reconcile.Result{}, nil
	}

	log.Info("syncing direct reflection from source")
	return reconcile.Result{}, r.patchReflection(ctx, src, obj, false)
}

func (r *ResourceReconciler[T]) applyReflection(ctx context.Context, src T, ns, name string, auto bool) error {
	desired := r.ops.Clone(src)
	desired.SetName(name)
	desired.SetNamespace(ns)
	desired.SetAnnotations(buildReflectionAnnotations(nil, src, auto))
	if err := r.Create(ctx, desired); err != nil {
		if !apierrors.IsAlreadyExists(err) && !apierrors.IsConflict(err) {
			return err
		}

		existing := r.ops.NewObject()
		if getErr := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, existing); getErr != nil {
			return client.IgnoreNotFound(getErr)
		}

		props := GetMirroringProperties(existing.GetAnnotations(), existing.GetResourceVersion())
		if props.Reflects != nil && props.Reflects.Namespace == src.GetNamespace() && props.Reflects.Name == src.GetName() {
			return r.patchReflection(ctx, src, existing, auto)
		}
		return nil
	}
	return nil
}

func (r *ResourceReconciler[T]) patchReflection(ctx context.Context, src, existing T, auto bool) error {
	base := existing.DeepCopyObject().(client.Object)
	existing.SetAnnotations(buildReflectionAnnotations(existing.GetAnnotations(), src, auto))
	r.ops.CopyData(src, existing)
	return r.Patch(ctx, existing, client.MergeFrom(base))
}

func (r *ResourceReconciler[T]) SetupWithManager(mgr ctrl.Manager) error {
	preds := r.ops.Predicates()

	builderRef := ctrl.NewControllerManagedBy(mgr).
		For(r.ops.NewObject(), predicatesFor(preds)...).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueSources),
			builder.WithPredicates(namespaceLabelChangedPredicate{}),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 4})

	return builderRef.Complete(r)
}

func predicatesFor(preds []predicate.Predicate) []builder.ForOption {
	if len(preds) == 0 {
		return nil
	}
	return []builder.ForOption{builder.WithPredicates(preds...)}
}

func (r *ResourceReconciler[T]) enqueueSources(ctx context.Context, _ client.Object) []reconcile.Request {
	list := r.ops.NewList()
	if err := r.List(ctx, list, client.MatchingFields{IndexIsAutoSource: "true"}); err != nil {
		r.log.Error(err, "failed to list auto-sources for namespace trigger")
		return nil
	}

	items := r.ops.ListItems(list)
	reqs := make([]reconcile.Request, 0, len(items))
	for _, item := range items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.GetNamespace(), Name: item.GetName()},
		})
	}
	return reqs
}

type namespaceLabelChangedPredicate struct{ predicate.Funcs }

func (namespaceLabelChangedPredicate) Update(e event.UpdateEvent) bool {
	return !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
}

func (namespaceLabelChangedPredicate) Create(_ event.CreateEvent) bool { return true }
func (namespaceLabelChangedPredicate) Delete(_ event.DeleteEvent) bool { return true }
func (namespaceLabelChangedPredicate) Generic(_ event.GenericEvent) bool {
	return false
}

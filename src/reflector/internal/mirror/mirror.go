package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/emberstack/kubernetes-reflector/internal/annotations"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ResourceOperations interface {
	ListAllWithName(ctx context.Context, name string) ([]metav1.Object, error)
	Get(ctx context.Context, ns, name string) (metav1.Object, error)
	Create(ctx context.Context, obj metav1.Object, ns string) error
	Patch(ctx context.Context, ns, name string, patchData []byte) error
	Delete(ctx context.Context, ns, name string) error
	Clone(src metav1.Object) (metav1.Object, error)
	DataPatchOps(src metav1.Object) []map[string]interface{}
	ResourceType() string
}

type ResourceMirror struct {
	ops            ResourceOperations
	logger         *zap.Logger
	listNamespaces func(ctx context.Context) ([]*corev1.Namespace, error)
	mu             sync.Mutex

	propertiesCache          map[NamespacedName]MirroringProperties
	autoSources              map[NamespacedName]bool
	directReflectionCache    map[NamespacedName]map[NamespacedName]struct{}
	autoReflectionCache      map[NamespacedName]map[NamespacedName]struct{}
	namespaceCache           map[string]*corev1.Namespace
	notFoundCache            map[NamespacedName]struct{}
	lastWarnedSelectorErrors map[NamespacedName]string
}

func NewResourceMirror(ops ResourceOperations, logger *zap.Logger, listNamespaces func(ctx context.Context) ([]*corev1.Namespace, error)) *ResourceMirror {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &ResourceMirror{
		ops:                      ops,
		logger:                   logger,
		listNamespaces:           listNamespaces,
		propertiesCache:          map[NamespacedName]MirroringProperties{},
		autoSources:              map[NamespacedName]bool{},
		directReflectionCache:    map[NamespacedName]map[NamespacedName]struct{}{},
		autoReflectionCache:      map[NamespacedName]map[NamespacedName]struct{}{},
		namespaceCache:           map[string]*corev1.Namespace{},
		notFoundCache:            map[NamespacedName]struct{}{},
		lastWarnedSelectorErrors: map[NamespacedName]string{},
	}
}

type EventType string

const (
	EventAdded    EventType = "ADDED"
	EventModified EventType = "MODIFIED"
	EventDeleted  EventType = "DELETED"
	EventError    EventType = "ERROR"
	EventBookmark EventType = "BOOKMARK"
)

type WatchEvent struct {
	Type   EventType
	Object interface{}
}

type WatcherClosedEvent struct {
	ResourceType string
	Faulted      bool
}

func (m *ResourceMirror) HandleClosed(e WatcherClosedEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e.ResourceType == "Namespace" {
		m.namespaceCache = map[string]*corev1.Namespace{}
		return
	}

	if e.ResourceType != m.ops.ResourceType() {
		return
	}

	m.propertiesCache = map[NamespacedName]MirroringProperties{}
	m.autoSources = map[NamespacedName]bool{}
	m.directReflectionCache = map[NamespacedName]map[NamespacedName]struct{}{}
	m.autoReflectionCache = map[NamespacedName]map[NamespacedName]struct{}{}
	m.notFoundCache = map[NamespacedName]struct{}{}
	m.lastWarnedSelectorErrors = map[NamespacedName]string{}
}

func (m *ResourceMirror) HandleEvent(ctx context.Context, e WatchEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch obj := e.Object.(type) {
	case *corev1.Secret:
		if m.ops.ResourceType() != "Secret" {
			return nil
		}
		return m.handleResourceEvent(ctx, e.Type, obj)
	case *corev1.ConfigMap:
		if m.ops.ResourceType() != "ConfigMap" {
			return nil
		}
		return m.handleResourceEvent(ctx, e.Type, obj)
	case *corev1.Namespace:
		return m.handleNamespaceEvent(ctx, e.Type, obj)
	default:
		return nil
	}
}

func (m *ResourceMirror) handleResourceEvent(ctx context.Context, eventType EventType, obj metav1.Object) error {
	objNsName := namespacedNameOf(obj)
	delete(m.notFoundCache, objNsName)

	switch eventType {
	case EventAdded, EventModified:
		return m.handleUpsert(ctx, obj, objNsName)
	case EventDeleted:
		delete(m.propertiesCache, objNsName)
		delete(m.lastWarnedSelectorErrors, objNsName)

		props := propertiesForObject(obj)
		if !props.IsReflection() {
			if props.Allowed && props.AutoEnabled {
				for reflectionNsName := range m.autoReflectionCache[objNsName] {
					if err := m.deleteResource(ctx, reflectionNsName); err != nil {
						return err
					}
				}
			}

			delete(m.autoSources, objNsName)
			delete(m.directReflectionCache, objNsName)
			delete(m.autoReflectionCache, objNsName)
			return nil
		}

		m.removeReflectionFromCache(m.directReflectionCache, objNsName)
		m.removeReflectionFromCache(m.autoReflectionCache, objNsName)
		return nil
	case EventError, EventBookmark:
		return nil
	default:
		return nil
	}
}

func (m *ResourceMirror) handleNamespaceEvent(ctx context.Context, eventType EventType, ns *corev1.Namespace) error {
	switch eventType {
	case EventAdded, EventModified:
		if eventType == EventModified {
			if cached, ok := m.namespaceCache[ns.Name]; ok && NamespaceLabelsEqual(cached, ns) {
				return nil
			}
		}

		m.namespaceCache[ns.Name] = ns.DeepCopy()

		for sourceNsName, isAutoSource := range m.autoSources {
			if !isAutoSource {
				continue
			}

			properties, ok := m.propertiesCache[sourceNsName]
			if !ok {
				continue
			}

			reflectionNsName := NamespacedName{Namespace: ns.Name, Name: sourceNsName.Name}
			if reflectionNsName == sourceNsName {
				continue
			}

			autoReflections := m.getOrCreateSet(m.autoReflectionCache, sourceNsName)
			if properties.CanBeAutoReflectedToNamespace(ns) {
				if err := m.resourceReflect(ctx, sourceNsName, reflectionNsName, nil, nil, true); err != nil {
					return err
				}
				autoReflections[reflectionNsName] = struct{}{}
			} else if _, exists := autoReflections[reflectionNsName]; exists {
				delete(autoReflections, reflectionNsName)
				m.logger.Debug("deleting auto-reflection because namespace no longer matches", zap.String("source", sourceNsName.String()), zap.String("reflection", reflectionNsName.String()))
				if err := m.deleteResource(ctx, reflectionNsName); err != nil {
					return err
				}
			}
		}

		for sourceNsName, reflections := range m.directReflectionCache {
			sourceProperties, ok := m.propertiesCache[sourceNsName]
			if !ok {
				continue
			}

			for reflectionNsName := range reflections {
				if reflectionNsName.Namespace != ns.Name {
					continue
				}
				if m.canBeReflectedToNamespaceCached(sourceProperties, ns.Name) {
					continue
				}
				delete(reflections, reflectionNsName)
				m.logger.Info("source no longer permits direct reflection", zap.String("source", sourceNsName.String()), zap.String("reflection", reflectionNsName.String()))
			}
		}

		return nil
	case EventDeleted:
		delete(m.namespaceCache, ns.Name)
		for sourceNsName, reflections := range m.autoReflectionCache {
			delete(reflections, NamespacedName{Namespace: ns.Name, Name: sourceNsName.Name})
		}
		for _, reflections := range m.directReflectionCache {
			for reflectionNsName := range reflections {
				if reflectionNsName.Namespace == ns.Name {
					delete(reflections, reflectionNsName)
				}
			}
		}
		return nil
	case EventError, EventBookmark:
		return nil
	default:
		return nil
	}
}

func (m *ResourceMirror) handleUpsert(ctx context.Context, obj metav1.Object, objNsName NamespacedName) error {
	objProperties := propertiesForObject(obj)
	m.propertiesCache[objNsName] = objProperties
	m.warnOnInvalidLabelSelectors(objNsName, objProperties)

	switch {
	case !objProperties.IsReflection():
		if reflections, ok := m.directReflectionCache[objNsName]; ok {
			for reflectionNsName := range reflections {
				if m.canBeReflectedToNamespaceCached(objProperties, reflectionNsName.Namespace) {
					continue
				}
				delete(reflections, reflectionNsName)
				m.logger.Info("source no longer permits direct reflection", zap.String("source", objNsName.String()), zap.String("reflection", reflectionNsName.String()))
			}
		}

		if reflections, ok := m.autoReflectionCache[objNsName]; ok {
			for reflectionNsName := range reflections {
				if m.canBeReflectedToNamespaceCached(objProperties, reflectionNsName.Namespace) {
					continue
				}
				delete(reflections, reflectionNsName)
				m.logger.Info("source no longer permits auto reflection; deleting reflection", zap.String("source", objNsName.String()), zap.String("reflection", reflectionNsName.String()))
				if err := m.deleteResource(ctx, reflectionNsName); err != nil {
					return err
				}
			}
		}

		isAutoSource := objProperties.Allowed && objProperties.AutoEnabled
		m.autoSources[objNsName] = isAutoSource
		if !isAutoSource {
			delete(m.autoReflectionCache, objNsName)
		}
		if !objProperties.Allowed {
			delete(m.directReflectionCache, objNsName)
			return nil
		}

		if reflections, ok := m.directReflectionCache[objNsName]; ok {
			for reflectionNsName := range reflections {
				reflectionProps, ok := m.propertiesCache[reflectionNsName]
				if !ok {
					delete(reflections, reflectionNsName)
					continue
				}
				if reflectionProps.ReflectedVersion == objProperties.ResourceVersion {
					m.logger.Debug("skipping direct reflection because versions match", zap.String("source", objNsName.String()), zap.String("reflection", reflectionNsName.String()))
					continue
				}
				if err := m.resourceReflect(ctx, objNsName, reflectionNsName, obj, nil, false); err != nil {
					return err
				}
			}
		}

		if isAutoSource {
			return m.autoReflectionForSource(ctx, objNsName, obj)
		}

		return nil
	case objProperties.Reflects != nil && !objProperties.IsAutoReflection:
		sourceNsName := *objProperties.Reflects
		sourceProperties, found, err := m.getSourceProperties(ctx, sourceNsName)
		if err != nil {
			return err
		}
		if !found {
			m.logger.Warn("could not update reflection because source was not found", zap.String("source", sourceNsName.String()), zap.String("reflection", objNsName.String()))
			return nil
		}

		m.propertiesCache[sourceNsName] = sourceProperties
		directReflections := m.getOrCreateSet(m.directReflectionCache, sourceNsName)
		directReflections[objNsName] = struct{}{}

		if !m.canBeReflectedToNamespaceCached(sourceProperties, objNsName.Namespace) {
			m.logger.Warn("source does not permit direct reflection", zap.String("source", sourceNsName.String()), zap.String("reflection", objNsName.String()))
			delete(directReflections, objNsName)
			return nil
		}
		if sourceProperties.ResourceVersion == objProperties.ReflectedVersion {
			m.logger.Debug("skipping direct reflection because versions match", zap.String("source", sourceNsName.String()), zap.String("reflection", objNsName.String()))
			return nil
		}

		return m.resourceReflect(ctx, sourceNsName, objNsName, nil, obj, false)
	case objProperties.Reflects != nil && objProperties.IsAutoReflection:
		sourceNsName := *objProperties.Reflects
		if _, knownMissing := m.notFoundCache[sourceNsName]; knownMissing {
			m.logger.Info("source no longer exists; deleting auto-reflection", zap.String("source", sourceNsName.String()), zap.String("reflection", objNsName.String()))
			return m.deleteResource(ctx, objNsName)
		}

		sourceProperties, found, err := m.getSourceProperties(ctx, sourceNsName)
		if err != nil {
			return err
		}
		if !found {
			m.notFoundCache[sourceNsName] = struct{}{}
			m.logger.Info("source no longer exists; deleting auto-reflection", zap.String("source", sourceNsName.String()), zap.String("reflection", objNsName.String()))
			return m.deleteResource(ctx, objNsName)
		}

		m.propertiesCache[sourceNsName] = sourceProperties
		if !m.canBeAutoReflectedToNamespaceCached(sourceProperties, objNsName.Namespace) {
			m.logger.Info("source no longer permits auto reflection; deleting reflection", zap.String("source", sourceNsName.String()), zap.String("reflection", objNsName.String()))
			return m.deleteResource(ctx, objNsName)
		}

		return nil
	default:
		return nil
	}
}

func (m *ResourceMirror) autoReflectionForSource(ctx context.Context, sourceNsName NamespacedName, sourceObj metav1.Object) error {
	sourceProperties, ok := m.propertiesCache[sourceNsName]
	if !ok {
		return nil
	}

	matches, err := m.ops.ListAllWithName(ctx, sourceNsName.Name)
	if err != nil {
		return err
	}
	namespaces, err := m.listNamespaces(ctx)
	if err != nil {
		return err
	}

	namespaceLookup := make(map[string]*corev1.Namespace, len(namespaces))
	for _, ns := range namespaces {
		if ns == nil {
			continue
		}
		copyNS := ns.DeepCopy()
		m.namespaceCache[copyNS.Name] = copyNS
		namespaceLookup[copyNS.Name] = copyNS
	}

	matchLookup := make(map[NamespacedName]metav1.Object, len(matches))
	for _, match := range matches {
		if match == nil {
			continue
		}
		nsName := namespacedNameOf(match)
		matchLookup[nsName] = match
		m.propertiesCache[nsName] = propertiesForObject(match)
	}

	toDelete := make([]NamespacedName, 0)
	toDeleteSet := map[NamespacedName]struct{}{}
	for nsName, match := range matchLookup {
		if nsName.Namespace == sourceNsName.Namespace {
			continue
		}

		matchProperties := propertiesForObject(match)
		if matchProperties.Reflects == nil || *matchProperties.Reflects != sourceNsName {
			continue
		}

		ns, ok := namespaceLookup[nsName.Namespace]
		if !ok || !sourceProperties.CanBeAutoReflectedToNamespace(ns) {
			toDelete = append(toDelete, nsName)
			toDeleteSet[nsName] = struct{}{}
		}
	}

	if sourceObj == nil {
		fetched, found, err := m.tryGet(ctx, sourceNsName)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		sourceObj = fetched
	}

	toCreate := make([]NamespacedName, 0)
	toCreateSet := map[NamespacedName]struct{}{}
	for _, ns := range namespaces {
		if ns == nil || ns.Name == sourceNsName.Namespace {
			continue
		}

		reflectionNsName := NamespacedName{Namespace: ns.Name, Name: sourceNsName.Name}
		if _, exists := matchLookup[reflectionNsName]; exists {
			continue
		}
		if sourceProperties.CanBeAutoReflectedToNamespace(ns) {
			toCreate = append(toCreate, reflectionNsName)
			toCreateSet[reflectionNsName] = struct{}{}
		}
	}

	toUpdate := make([]NamespacedName, 0)
	toSkip := make([]NamespacedName, 0)
	for nsName, match := range matchLookup {
		if nsName.Namespace == sourceNsName.Namespace {
			continue
		}
		if _, deleting := toDeleteSet[nsName]; deleting {
			continue
		}
		if _, creating := toCreateSet[nsName]; creating {
			continue
		}

		matchProperties := propertiesForObject(match)
		if matchProperties.Reflects == nil || *matchProperties.Reflects != sourceNsName {
			continue
		}
		if matchProperties.ReflectedVersion != sourceProperties.ResourceVersion {
			toUpdate = append(toUpdate, nsName)
		} else {
			toSkip = append(toSkip, nsName)
		}
	}

	reflections := map[NamespacedName]struct{}{}
	for _, item := range toCreate {
		reflections[item] = struct{}{}
	}
	for _, item := range toUpdate {
		reflections[item] = struct{}{}
	}
	for _, item := range toSkip {
		reflections[item] = struct{}{}
	}
	m.autoReflectionCache[sourceNsName] = reflections

	for _, reflectionNsName := range toDelete {
		if err := m.deleteResource(ctx, reflectionNsName); err != nil {
			return err
		}
	}
	for _, reflectionNsName := range toCreate {
		if err := m.resourceReflect(ctx, sourceNsName, reflectionNsName, sourceObj, nil, true); err != nil {
			return err
		}
	}
	for _, reflectionNsName := range toUpdate {
		if err := m.resourceReflect(ctx, sourceNsName, reflectionNsName, sourceObj, matchLookup[reflectionNsName], true); err != nil {
			return err
		}
	}

	m.logger.Info(fmt.Sprintf("Auto-reflected %s. Created %d - Updated %d - Deleted %d - Validated %d.", sourceNsName.String(), len(toCreate), len(toUpdate), len(toDelete), len(toSkip)))
	return nil
}

func (m *ResourceMirror) resourceReflect(ctx context.Context, sourceNsName, reflectionNsName NamespacedName, sourceObj metav1.Object, reflectionObj metav1.Object, autoReflection bool) error {
	if sourceNsName == reflectionNsName {
		return nil
	}

	m.logger.Debug("reflecting resource", zap.String("source", sourceNsName.String()), zap.String("reflection", reflectionNsName.String()))

	if sourceObj == nil {
		fetched, found, err := m.tryGet(ctx, sourceNsName)
		if err != nil {
			return err
		}
		if !found {
			m.logger.Warn("could not reflect because source was not found", zap.String("source", sourceNsName.String()), zap.String("reflection", reflectionNsName.String()))
			return nil
		}
		sourceObj = fetched
	}

	patchAnnotations := map[string]string{
		annotations.MetaAutoReflects:     fmt.Sprintf("%t", autoReflection),
		annotations.Reflects:             sourceNsName.String(),
		annotations.MetaReflectedVersion: sourceObj.GetResourceVersion(),
		annotations.MetaReflectedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	if reflectionObj == nil {
		clone, err := m.ops.Clone(sourceObj)
		if err != nil {
			return err
		}
		clone.SetName(reflectionNsName.Name)
		clone.SetNamespace(reflectionNsName.Namespace)
		clone.SetAnnotations(mergeAnnotations(clone.GetAnnotations(), patchAnnotations))

		err = m.ops.Create(ctx, clone, reflectionNsName.Namespace)
		if err == nil {
			m.logger.Info("created reflection", zap.String("source", sourceNsName.String()), zap.String("reflection", reflectionNsName.String()))
			return nil
		}
		if !apierrors.IsConflict(err) && !apierrors.IsAlreadyExists(err) {
			return err
		}

		fetched, found, err := m.tryGet(ctx, reflectionNsName)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		reflectionObj = fetched
	}

	reflectionProperties := propertiesForObject(reflectionObj)
	if reflectionProperties.ReflectedVersion == sourceObj.GetResourceVersion() {
		m.logger.Debug("skipping reflection because versions match", zap.String("source", sourceNsName.String()), zap.String("reflection", reflectionNsName.String()))
		return nil
	}

	patchOps := []map[string]interface{}{{
		"op":    "replace",
		"path":  "/metadata/annotations",
		"value": mergeAnnotations(reflectionObj.GetAnnotations(), patchAnnotations),
	}}
	patchOps = append(patchOps, m.ops.DataPatchOps(sourceObj)...)

	patchData, err := json.Marshal(patchOps)
	if err != nil {
		return err
	}
	if err := m.ops.Patch(ctx, reflectionNsName.Namespace, reflectionNsName.Name, patchData); err != nil {
		return err
	}

	m.logger.Info("patched reflection", zap.String("source", sourceNsName.String()), zap.String("reflection", reflectionNsName.String()))
	return nil
}

func (m *ResourceMirror) canBeReflectedToNamespaceCached(properties MirroringProperties, ns string) bool {
	if nsObj, ok := m.namespaceCache[ns]; ok {
		return properties.CanBeReflectedToNamespace(nsObj)
	}
	if properties.AllowedNamespacesSelector != "" {
		m.logger.Debug("namespace not in cache; denying reflection because selector is configured", zap.String("namespace", ns))
		return false
	}
	return properties.CanBeReflectedToNamespaceByName(ns)
}

func (m *ResourceMirror) canBeAutoReflectedToNamespaceCached(properties MirroringProperties, ns string) bool {
	if nsObj, ok := m.namespaceCache[ns]; ok {
		return properties.CanBeAutoReflectedToNamespace(nsObj)
	}
	if properties.AllowedNamespacesSelector != "" || properties.AutoNamespacesSelector != "" {
		m.logger.Debug("namespace not in cache; denying auto reflection because selector is configured", zap.String("namespace", ns))
		return false
	}
	return properties.CanBeAutoReflectedToNamespaceByName(ns)
}

func (m *ResourceMirror) warnOnInvalidLabelSelectors(sourceNsName NamespacedName, properties MirroringProperties) {
	errs := GetLabelSelectorValidationErrors(properties)
	if len(errs) == 0 {
		delete(m.lastWarnedSelectorErrors, sourceNsName)
		return
	}

	signature := fmt.Sprint(errs)
	if previous, ok := m.lastWarnedSelectorErrors[sourceNsName]; ok && previous == signature {
		return
	}

	m.lastWarnedSelectorErrors[sourceNsName] = signature
	for _, err := range errs {
		m.logger.Warn("invalid label selector on source", zap.String("source", sourceNsName.String()), zap.String("error", err))
	}
}

func (m *ResourceMirror) getSourceProperties(ctx context.Context, sourceNsName NamespacedName) (MirroringProperties, bool, error) {
	if props, ok := m.propertiesCache[sourceNsName]; ok {
		return props, true, nil
	}

	obj, found, err := m.tryGet(ctx, sourceNsName)
	if err != nil || !found {
		return MirroringProperties{}, found, err
	}

	props := propertiesForObject(obj)
	return props, true, nil
}

func (m *ResourceMirror) tryGet(ctx context.Context, resourceNsName NamespacedName) (metav1.Object, bool, error) {
	obj, err := m.ops.Get(ctx, resourceNsName.Namespace, resourceNsName.Name)
	if err == nil {
		delete(m.notFoundCache, resourceNsName)
		return obj, true, nil
	}
	if apierrors.IsNotFound(err) {
		m.notFoundCache[resourceNsName] = struct{}{}
		return nil, false, nil
	}
	return nil, false, err
}

func (m *ResourceMirror) deleteResource(ctx context.Context, resourceNsName NamespacedName) error {
	err := m.ops.Delete(ctx, resourceNsName.Namespace, resourceNsName.Name)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (m *ResourceMirror) removeReflectionFromCache(cache map[NamespacedName]map[NamespacedName]struct{}, reflection NamespacedName) {
	for _, reflections := range cache {
		delete(reflections, reflection)
	}
}

func (m *ResourceMirror) getOrCreateSet(cache map[NamespacedName]map[NamespacedName]struct{}, source NamespacedName) map[NamespacedName]struct{} {
	if cache[source] == nil {
		cache[source] = map[NamespacedName]struct{}{}
	}
	return cache[source]
}

func namespacedNameOf(obj metav1.Object) NamespacedName {
	return NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}

func propertiesForObject(obj metav1.Object) MirroringProperties {
	meta := &metav1.ObjectMeta{Annotations: obj.GetAnnotations(), ResourceVersion: obj.GetResourceVersion()}
	return GetMirroringProperties(meta)
}

func mergeAnnotations(existing, overlay map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range overlay {
		merged[key] = value
	}
	return merged
}

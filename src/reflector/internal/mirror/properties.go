package mirror

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emberstack/kubernetes-reflector/internal/annotations"
	"github.com/emberstack/kubernetes-reflector/internal/selector"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NamespacedName struct {
	Namespace string
	Name      string
}

func (n NamespacedName) String() string {
	return n.Namespace + "/" + n.Name
}

func ParseNamespacedName(s string) (NamespacedName, bool) {
	if strings.Count(s, "/") != 1 {
		return NamespacedName{}, false
	}

	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return NamespacedName{}, false
	}

	return NamespacedName{Namespace: parts[0], Name: parts[1]}, true
}

type MirroringProperties struct {
	Allowed                   bool
	AllowedNamespaces         string
	AllowedNamespacesSelector string
	AutoEnabled               bool
	AutoNamespaces            string
	AutoNamespacesSelector    string
	Reflects                  *NamespacedName
	ResourceVersion           string
	IsAutoReflection          bool
	ReflectedVersion          string
	ReflectedAt               *time.Time
}

func (p MirroringProperties) IsReflection() bool {
	return p.Reflects != nil
}

func GetMirroringProperties(meta *metav1.ObjectMeta) MirroringProperties {
	if meta == nil {
		meta = &metav1.ObjectMeta{}
	}

	anns := meta.Annotations
	props := MirroringProperties{
		Allowed:                   parseAnnotationBool(anns, annotations.ReflectionAllowed),
		AllowedNamespaces:         annotationString(anns, annotations.ReflectionAllowedNamespaces),
		AllowedNamespacesSelector: annotationString(anns, annotations.ReflectionAllowedNsSelector),
		AutoEnabled:               parseAnnotationBool(anns, annotations.ReflectionAutoEnabled),
		AutoNamespaces:            annotationString(anns, annotations.ReflectionAutoNamespaces),
		AutoNamespacesSelector:    annotationString(anns, annotations.ReflectionAutoNsSelector),
		ResourceVersion:           meta.ResourceVersion,
		IsAutoReflection:          parseAnnotationBool(anns, annotations.MetaAutoReflects),
		ReflectedVersion:          annotationString(anns, annotations.MetaReflectedVersion),
	}

	if value := annotationString(anns, annotations.Reflects); value != "" {
		if parsed, ok := ParseNamespacedName(value); ok {
			props.Reflects = &parsed
		}
	}

	if value := annotationString(anns, annotations.MetaReflectedAt); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			props.ReflectedAt = &parsed
		}
	}

	return props
}

func (p MirroringProperties) CanBeReflectedToNamespaceByName(ns string) bool {
	return p.Allowed && patternListMatch(p.AllowedNamespaces, ns)
}

func (p MirroringProperties) CanBeReflectedToNamespace(ns *corev1.Namespace) bool {
	return p.Allowed && matchNamespace(p.AllowedNamespaces, p.AllowedNamespacesSelector, ns)
}

func (p MirroringProperties) CanBeAutoReflectedToNamespaceByName(ns string) bool {
	return p.CanBeReflectedToNamespaceByName(ns) && p.AutoEnabled && patternListMatch(p.AutoNamespaces, ns)
}

func (p MirroringProperties) CanBeAutoReflectedToNamespace(ns *corev1.Namespace) bool {
	return p.CanBeReflectedToNamespace(ns) && p.AutoEnabled && matchNamespace(p.AutoNamespaces, p.AutoNamespacesSelector, ns)
}

func GetLabelSelectorValidationErrors(p MirroringProperties) []string {
	var errs []string
	if selectorErrs := selector.GetLabelSelectorErrors(annotations.ReflectionAllowedNsSelector, p.AllowedNamespacesSelector); len(selectorErrs) > 0 {
		errs = append(errs, selectorErrs...)
	}
	if selectorErrs := selector.GetLabelSelectorErrors(annotations.ReflectionAutoNsSelector, p.AutoNamespacesSelector); len(selectorErrs) > 0 {
		errs = append(errs, selectorErrs...)
	}
	return errs
}

func NamespaceLabelsEqual(a, b *corev1.Namespace) bool {
	labelsA := namespaceLabels(a)
	labelsB := namespaceLabels(b)
	if len(labelsA) != len(labelsB) {
		return false
	}
	for key, value := range labelsA {
		if labelsB[key] != value {
			return false
		}
	}
	return true
}

func patternListMatch(patternList, value string) bool {
	if patternList == "" {
		return true
	}

	matchedAnyPattern := false
	for _, pattern := range strings.Split(patternList, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		matchedAnyPattern = true
		matched, err := regexp.MatchString("^"+pattern+"$", value)
		if err == nil && matched {
			return true
		}
	}

	if !matchedAnyPattern {
		return false
	}

	return false
}

func matchNamespace(patternList, labelSelector string, ns *corev1.Namespace) bool {
	hasPatterns := patternList != ""
	hasSelector := strings.TrimSpace(labelSelector) != ""
	if !hasPatterns && !hasSelector {
		return true
	}
	if ns == nil {
		return false
	}
	if hasPatterns && patternListMatch(patternList, ns.Name) {
		return true
	}
	if hasSelector && selector.LabelSelectorMatch(labelSelector, ns) {
		return true
	}
	return false
}

func parseAnnotationBool(annotationsMap map[string]string, key string) bool {
	value, ok := annotationsMap[key]
	if !ok {
		return false
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && parsed
}

func annotationString(annotationsMap map[string]string, key string) string {
	if annotationsMap == nil {
		return ""
	}
	return strings.TrimSpace(annotationsMap[key])
}

func namespaceLabels(ns *corev1.Namespace) map[string]string {
	if ns == nil || len(ns.Labels) == 0 {
		return map[string]string{}
	}
	return ns.Labels
}

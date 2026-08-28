package mirror

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emberstack/kubernetes-reflector/internal/annotations"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type NamespacedName = types.NamespacedName

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

func (p MirroringProperties) IsReflection() bool { return p.Reflects != nil }

func GetMirroringProperties(annots map[string]string, resourceVersion string) MirroringProperties {
	get := func(key string) string {
		if annots == nil {
			return ""
		}
		return annots[key]
	}
	parseBool := func(key string) bool {
		v, _ := strconv.ParseBool(get(key))
		return v
	}
	strVal := func(key string) string {
		return strings.TrimSpace(get(key))
	}

	var reflects *NamespacedName
	if r := strVal(annotations.Reflects); r != "" {
		parts := strings.SplitN(r, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			nn := NamespacedName{Namespace: parts[0], Name: parts[1]}
			reflects = &nn
		}
	}

	var reflectedAt *time.Time
	if s := strVal(annotations.MetaReflectedAt); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err == nil {
			reflectedAt = &t
		}
	}

	reflectedVersion := strVal(annotations.MetaReflectedVersion)

	return MirroringProperties{
		Allowed:                   parseBool(annotations.ReflectionAllowed),
		AllowedNamespaces:         strVal(annotations.ReflectionAllowedNamespaces),
		AllowedNamespacesSelector: strVal(annotations.ReflectionAllowedNsSelector),
		AutoEnabled:               parseBool(annotations.ReflectionAutoEnabled),
		AutoNamespaces:            strVal(annotations.ReflectionAutoNamespaces),
		AutoNamespacesSelector:    strVal(annotations.ReflectionAutoNsSelector),
		Reflects:                  reflects,
		ResourceVersion:           resourceVersion,
		IsAutoReflection:          parseBool(annotations.MetaAutoReflects),
		ReflectedVersion:          reflectedVersion,
		ReflectedAt:               reflectedAt,
	}
}

func (p MirroringProperties) CanBeReflectedToNamespace(ns *corev1.Namespace) bool {
	if !p.Allowed {
		return false
	}
	return matchNamespace(p.AllowedNamespaces, p.AllowedNamespacesSelector, ns)
}

func (p MirroringProperties) CanBeAutoReflectedToNamespace(ns *corev1.Namespace) bool {
	return p.CanBeReflectedToNamespace(ns) && p.AutoEnabled &&
		matchNamespace(p.AutoNamespaces, p.AutoNamespacesSelector, ns)
}

func NamespaceLabelsEqual(a, b *corev1.Namespace) bool {
	al := a.GetLabels()
	bl := b.GetLabels()
	if len(al) != len(bl) {
		return false
	}
	for k, v := range al {
		if bl[k] != v {
			return false
		}
	}
	return true
}

func patternListMatch(patternList, value string) bool {
	if strings.TrimSpace(patternList) == "" {
		return true
	}
	for _, p := range strings.Split(patternList, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		m, err := regexp.MatchString("^(?:"+p+")$", value)
		if err == nil && m {
			return true
		}
	}
	return false
}

func matchNamespace(patternList, labelSelector string, ns *corev1.Namespace) bool {
	hasPatterns := strings.TrimSpace(patternList) != ""
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
	if hasSelector && LabelSelectorMatch(labelSelector, ns) {
		return true
	}
	return false
}

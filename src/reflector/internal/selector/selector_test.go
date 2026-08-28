package selector_test

import (
	"strings"
	"testing"

	"github.com/emberstack/kubernetes-reflector/internal/selector"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func namespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func TestLabelSelectorMatchEmptySelectorMatchesAll(t *testing.T) {
	ns := namespace("test", nil)
	if !selector.LabelSelectorMatch("", ns) {
		t.Fatal("expected empty selector to match")
	}
	if !selector.LabelSelectorMatch("  ", ns) {
		t.Fatal("expected whitespace selector to match")
	}
}

func TestLabelSelectorMatchEquality(t *testing.T) {
	if !selector.LabelSelectorMatch("env=production", namespace("test", map[string]string{"env": "production"})) {
		t.Fatal("expected equality selector to match")
	}
	if selector.LabelSelectorMatch("env=production", namespace("test", map[string]string{"env": "staging"})) {
		t.Fatal("expected equality selector not to match")
	}
}

func TestLabelSelectorMatchDoubleEquality(t *testing.T) {
	if !selector.LabelSelectorMatch("env==production", namespace("test", map[string]string{"env": "production"})) {
		t.Fatal("expected double equality selector to match")
	}
}

func TestLabelSelectorMatchInequality(t *testing.T) {
	if !selector.LabelSelectorMatch("env!=production", namespace("test", map[string]string{"env": "staging"})) {
		t.Fatal("expected inequality selector to match")
	}
	if selector.LabelSelectorMatch("env!=production", namespace("test", map[string]string{"env": "production"})) {
		t.Fatal("expected inequality selector not to match")
	}
	if !selector.LabelSelectorMatch("env!=production", namespace("test", nil)) {
		t.Fatal("expected missing label to satisfy inequality")
	}
}

func TestLabelSelectorMatchExistence(t *testing.T) {
	if !selector.LabelSelectorMatch("env", namespace("test", map[string]string{"env": "production"})) {
		t.Fatal("expected existence selector to match")
	}
	if selector.LabelSelectorMatch("env", namespace("test", nil)) {
		t.Fatal("expected existence selector not to match")
	}
}

func TestLabelSelectorMatchDoesNotExist(t *testing.T) {
	if !selector.LabelSelectorMatch("!env", namespace("test", nil)) {
		t.Fatal("expected !env to match")
	}
	if selector.LabelSelectorMatch("!env", namespace("test", map[string]string{"env": "production"})) {
		t.Fatal("expected !env not to match")
	}
}

func TestLabelSelectorMatchMultipleRequirements(t *testing.T) {
	ns := namespace("test", map[string]string{"env": "production", "tier": "frontend"})
	if !selector.LabelSelectorMatch("env=production,tier=frontend", ns) {
		t.Fatal("expected both requirements to match")
	}
	ns = namespace("test", map[string]string{"env": "production", "tier": "backend"})
	if selector.LabelSelectorMatch("env=production,tier=frontend", ns) {
		t.Fatal("expected one failing requirement to fail selector")
	}
}

func TestLabelSelectorMatchInAndNotIn(t *testing.T) {
	if !selector.LabelSelectorMatch("env in (production,staging)", namespace("test", map[string]string{"env": "production"})) {
		t.Fatal("expected in selector to match production")
	}
	if !selector.LabelSelectorMatch("env in (production,staging)", namespace("test", map[string]string{"env": "staging"})) {
		t.Fatal("expected in selector to match staging")
	}
	if selector.LabelSelectorMatch("env in (production,staging)", namespace("test", map[string]string{"env": "dev"})) {
		t.Fatal("expected in selector not to match dev")
	}
	if !selector.LabelSelectorMatch("env notin (production,staging)", namespace("test", map[string]string{"env": "dev"})) {
		t.Fatal("expected notin selector to match dev")
	}
	if selector.LabelSelectorMatch("env notin (production,staging)", namespace("test", map[string]string{"env": "production"})) {
		t.Fatal("expected notin selector not to match production")
	}
}

func TestLabelSelectorMatchInvalidSelectorsFailClosed(t *testing.T) {
	validNS := namespace("test", map[string]string{"env": "production"})
	invalidSelectors := []string{",", ",,", ", ,", "!", "!=value", "=value", "env in (prod", "env in prod)", "env in ()", "()", "=", "-env=prod", "env-=prod", ".env=prod", "env name=prod", strings.Repeat("a", 64) + "=value"}
	for _, selectorStr := range invalidSelectors {
		if selector.LabelSelectorMatch(selectorStr, validNS) {
			t.Fatalf("expected invalid selector %q to fail closed", selectorStr)
		}
	}
}

func TestLabelSelectorMatchPrefixedKey(t *testing.T) {
	ns := namespace("test", map[string]string{"app.kubernetes.io/name": "reflector"})
	if !selector.LabelSelectorMatch("app.kubernetes.io/name=reflector", ns) {
		t.Fatal("expected prefixed key to match")
	}
}

func TestGetLabelSelectorErrors(t *testing.T) {
	if errs := selector.GetLabelSelectorErrors("annotation", "env=prod"); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	errs := selector.GetLabelSelectorErrors("reflector.v1.k8s.emberstack.com/reflection-allowed-namespaces-selector", "=prod")
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}
	if !strings.Contains(errs[0], "reflection-allowed-namespaces-selector") {
		t.Fatalf("expected annotation name in error, got %v", errs)
	}
}

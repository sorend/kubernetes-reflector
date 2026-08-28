package mirror_test

import (
	"testing"

	"github.com/emberstack/kubernetes-reflector/internal/mirror"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ns(labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns", Labels: labels}}
}

func TestNamespaceLabelsEqualNilAndEmpty(t *testing.T) {
	if !mirror.NamespaceLabelsEqual(ns(nil), ns(map[string]string{})) {
		t.Fatal("expected nil and empty labels to be equal")
	}
	if !mirror.NamespaceLabelsEqual(ns(map[string]string{}), ns(nil)) {
		t.Fatal("expected empty and nil labels to be equal")
	}
	if !mirror.NamespaceLabelsEqual(ns(nil), ns(nil)) {
		t.Fatal("expected nil labels to be equal")
	}
}

func TestNamespaceLabelsEqualSameLabels(t *testing.T) {
	labels := map[string]string{"env": "prod", "tier": "frontend"}
	if !mirror.NamespaceLabelsEqual(ns(labels), ns(map[string]string{"env": "prod", "tier": "frontend"})) {
		t.Fatal("expected labels to be equal")
	}
}

func TestNamespaceLabelsEqualChangedValue(t *testing.T) {
	if mirror.NamespaceLabelsEqual(ns(map[string]string{"env": "prod"}), ns(map[string]string{"env": "staging"})) {
		t.Fatal("expected changed value to be unequal")
	}
}

func TestNamespaceLabelsEqualAddedLabel(t *testing.T) {
	if mirror.NamespaceLabelsEqual(ns(map[string]string{"env": "prod"}), ns(map[string]string{"env": "prod", "tier": "frontend"})) {
		t.Fatal("expected added label to be unequal")
	}
}

func TestNamespaceLabelsEqualRemovedLabel(t *testing.T) {
	if mirror.NamespaceLabelsEqual(ns(map[string]string{"env": "prod", "tier": "frontend"}), ns(map[string]string{"env": "prod"})) {
		t.Fatal("expected removed label to be unequal")
	}
}

func TestNamespaceLabelsEqualRenamedKey(t *testing.T) {
	if mirror.NamespaceLabelsEqual(ns(map[string]string{"env": "prod"}), ns(map[string]string{"environment": "prod"})) {
		t.Fatal("expected renamed key to be unequal")
	}
}

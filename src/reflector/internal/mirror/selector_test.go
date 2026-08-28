package mirror_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sorend/kubernetes-reflector/internal/mirror"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func namespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

var _ = Describe("LabelSelectorMatch", func() {
	It("matches empty selectors", func() {
		ns := namespace("test", nil)
		Expect(mirror.LabelSelectorMatch("", ns)).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("  ", ns)).To(BeTrue())
	})

	It("supports equality and double equality", func() {
		Expect(mirror.LabelSelectorMatch("env=production", namespace("test", map[string]string{"env": "production"}))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("env==production", namespace("test", map[string]string{"env": "production"}))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("env=production", namespace("test", map[string]string{"env": "staging"}))).To(BeFalse())
	})

	It("supports inequality", func() {
		Expect(mirror.LabelSelectorMatch("env!=production", namespace("test", map[string]string{"env": "staging"}))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("env!=production", namespace("test", map[string]string{"env": "production"}))).To(BeFalse())
		Expect(mirror.LabelSelectorMatch("env!=production", namespace("test", nil))).To(BeTrue())
	})

	It("supports existence and non-existence", func() {
		Expect(mirror.LabelSelectorMatch("env", namespace("test", map[string]string{"env": "production"}))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("env", namespace("test", nil))).To(BeFalse())
		Expect(mirror.LabelSelectorMatch("!env", namespace("test", nil))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("!env", namespace("test", map[string]string{"env": "production"}))).To(BeFalse())
	})

	It("supports multiple requirements", func() {
		Expect(mirror.LabelSelectorMatch("env=production,tier=frontend", namespace("test", map[string]string{"env": "production", "tier": "frontend"}))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("env=production,tier=frontend", namespace("test", map[string]string{"env": "production", "tier": "backend"}))).To(BeFalse())
	})

	It("supports in and notin", func() {
		Expect(mirror.LabelSelectorMatch("env in (production,staging)", namespace("test", map[string]string{"env": "production"}))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("env in (production,staging)", namespace("test", map[string]string{"env": "staging"}))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("env in (production,staging)", namespace("test", map[string]string{"env": "dev"}))).To(BeFalse())
		Expect(mirror.LabelSelectorMatch("env notin (production,staging)", namespace("test", map[string]string{"env": "dev"}))).To(BeTrue())
		Expect(mirror.LabelSelectorMatch("env notin (production,staging)", namespace("test", map[string]string{"env": "production"}))).To(BeFalse())
	})

	It("fails closed for invalid selectors", func() {
		validNS := namespace("test", map[string]string{"env": "production"})
		invalidSelectors := []string{",", ",,", ", ,", "!", "!=value", "=value", "env in (prod", "env in prod)", "env in ()", "()", "=", "-env=prod", "env-=prod", ".env=prod", "env name=prod", strings.Repeat("a", 64) + "=value"}
		for _, selectorStr := range invalidSelectors {
			Expect(mirror.LabelSelectorMatch(selectorStr, validNS)).To(BeFalse(), selectorStr)
		}
	})

	It("supports prefixed keys", func() {
		ns := namespace("test", map[string]string{"app.kubernetes.io/name": "reflector"})
		Expect(mirror.LabelSelectorMatch("app.kubernetes.io/name=reflector", ns)).To(BeTrue())
	})

	It("returns selector validation errors", func() {
		Expect(mirror.GetLabelSelectorErrors("annotation", "env=prod")).To(BeEmpty())
		errs := mirror.GetLabelSelectorErrors("reflector.v2.sorend.github.com/reflection-allowed-namespaces-selector", "=prod")
		Expect(errs).NotTo(BeEmpty())
		Expect(errs[0]).To(ContainSubstring("reflection-allowed-namespaces-selector"))
	})
})

package mirror_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sorend/kubernetes-reflector/internal/annotations"
	"github.com/sorend/kubernetes-reflector/internal/mirror"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ns(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

var _ = Describe("MirroringProperties", func() {
	Describe("GetMirroringProperties", func() {
		It("extracts all supported annotations", func() {
			ts := time.Now().UTC().Truncate(time.Second)
			props := mirror.GetMirroringProperties(map[string]string{
				annotations.ReflectionAllowed:           "true",
				annotations.ReflectionAllowedNamespaces: "team-a,team-b",
				annotations.ReflectionAllowedNsSelector: "env=prod",
				annotations.ReflectionAutoEnabled:       "true",
				annotations.ReflectionAutoNamespaces:    "dev-.*",
				annotations.ReflectionAutoNsSelector:    "tier=shared",
				annotations.Reflects:                    "source-ns/source-name",
				annotations.MetaAutoReflects:            "true",
				annotations.MetaReflectedVersion:        "42",
				annotations.MetaReflectedAt:             ts.Format(time.RFC3339),
			}, "99")

			Expect(props.Allowed).To(BeTrue())
			Expect(props.AllowedNamespaces).To(Equal("team-a,team-b"))
			Expect(props.AllowedNamespacesSelector).To(Equal("env=prod"))
			Expect(props.AutoEnabled).To(BeTrue())
			Expect(props.AutoNamespaces).To(Equal("dev-.*"))
			Expect(props.AutoNamespacesSelector).To(Equal("tier=shared"))
			Expect(props.Reflects).NotTo(BeNil())
			Expect(*props.Reflects).To(Equal(mirror.NamespacedName{Namespace: "source-ns", Name: "source-name"}))
			Expect(props.ResourceVersion).To(Equal("99"))
			Expect(props.IsAutoReflection).To(BeTrue())
			Expect(props.ReflectedVersion).To(Equal("42"))
			Expect(props.ReflectedAt).NotTo(BeNil())
			Expect(props.ReflectedAt.UTC()).To(Equal(ts))
		})

		It("ignores malformed reflection metadata", func() {
			props := mirror.GetMirroringProperties(map[string]string{
				annotations.ReflectionAllowed: " definitely-not-a-bool ",
				annotations.Reflects:          "missing-slash",
				annotations.MetaReflectedAt:   "not-a-time",
			}, "7")

			Expect(props.Allowed).To(BeFalse())
			Expect(props.Reflects).To(BeNil())
			Expect(props.ReflectedAt).To(BeNil())
			Expect(props.ResourceVersion).To(Equal("7"))
		})
	})

	Describe("CanBeReflectedToNamespace", func() {
		It("requires reflection to be allowed", func() {
			props := mirror.MirroringProperties{}
			Expect(props.CanBeReflectedToNamespace(ns("target", nil))).To(BeFalse())
		})

		It("allows all namespaces when filters are empty", func() {
			props := mirror.MirroringProperties{Allowed: true}
			Expect(props.CanBeReflectedToNamespace(ns("target", nil))).To(BeTrue())
		})

		It("matches namespace names by anchored regex list", func() {
			props := mirror.MirroringProperties{Allowed: true, AllowedNamespaces: "prod-.*,shared"}
			Expect(props.CanBeReflectedToNamespace(ns("prod-a", nil))).To(BeTrue())
			Expect(props.CanBeReflectedToNamespace(ns("shared", nil))).To(BeTrue())
			Expect(props.CanBeReflectedToNamespace(ns("prod-a-extra", nil))).To(BeTrue())
			Expect(props.CanBeReflectedToNamespace(ns("qa", nil))).To(BeFalse())
		})

		It("supports OR semantics between names and selectors", func() {
			props := mirror.MirroringProperties{Allowed: true, AllowedNamespaces: "exact-name", AllowedNamespacesSelector: "team=platform"}
			Expect(props.CanBeReflectedToNamespace(ns("exact-name", nil))).To(BeTrue())
			Expect(props.CanBeReflectedToNamespace(ns("other", map[string]string{"team": "platform"}))).To(BeTrue())
			Expect(props.CanBeReflectedToNamespace(ns("other", map[string]string{"team": "app"}))).To(BeFalse())
		})

		It("fails closed on invalid regex when it is the only filter", func() {
			props := mirror.MirroringProperties{Allowed: true, AllowedNamespaces: "["}
			Expect(props.CanBeReflectedToNamespace(ns("target", nil))).To(BeFalse())
		})
	})

	Describe("CanBeAutoReflectedToNamespace", func() {
		It("requires allowed reflection and auto enabled", func() {
			props := mirror.MirroringProperties{Allowed: true, AutoEnabled: false}
			Expect(props.CanBeAutoReflectedToNamespace(ns("target", nil))).To(BeFalse())
		})

		It("applies auto filters after allowed filters", func() {
			props := mirror.MirroringProperties{
				Allowed:                true,
				AllowedNamespaces:      "team-.*",
				AutoEnabled:            true,
				AutoNamespacesSelector: "mirror=true",
			}
			Expect(props.CanBeAutoReflectedToNamespace(ns("team-a", map[string]string{"mirror": "true"}))).To(BeTrue())
			Expect(props.CanBeAutoReflectedToNamespace(ns("team-a", map[string]string{"mirror": "false"}))).To(BeFalse())
			Expect(props.CanBeAutoReflectedToNamespace(ns("other", map[string]string{"mirror": "true"}))).To(BeFalse())
		})
	})

	Describe("NamespaceLabelsEqual", func() {
		It("treats nil and empty label maps as equal", func() {
			Expect(mirror.NamespaceLabelsEqual(ns("a", nil), ns("a", map[string]string{}))).To(BeTrue())
			Expect(mirror.NamespaceLabelsEqual(ns("a", map[string]string{}), ns("a", nil))).To(BeTrue())
		})

		It("detects changed label sets", func() {
			Expect(mirror.NamespaceLabelsEqual(ns("a", map[string]string{"env": "prod"}), ns("a", map[string]string{"env": "staging"}))).To(BeFalse())
			Expect(mirror.NamespaceLabelsEqual(ns("a", map[string]string{"env": "prod"}), ns("a", map[string]string{"env": "prod", "tier": "frontend"}))).To(BeFalse())
			Expect(mirror.NamespaceLabelsEqual(ns("a", map[string]string{"env": "prod"}), ns("a", map[string]string{"environment": "prod"}))).To(BeFalse())
		})
	})
})

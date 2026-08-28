package mirror_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sorend/kubernetes-reflector/internal/mirror"
)

var _ = Describe("GlobMatcher", func() {
	Describe("ParseGlobPatterns", func() {
		It("returns empty for nil, empty, and whitespace input", func() {
			Expect(mirror.ParseGlobPatterns("")).To(BeEmpty())
			Expect(mirror.ParseGlobPatterns("   ")).To(BeEmpty())
		})

		It("parses a single pattern", func() {
			Expect(mirror.ParseGlobPatterns("kube-system")).To(HaveLen(1))
		})

		It("parses multiple patterns", func() {
			Expect(mirror.ParseGlobPatterns("kube-system,kube-public,default")).To(HaveLen(3))
		})

		It("trims and ignores empty entries", func() {
			Expect(mirror.ParseGlobPatterns(" kube-system , , kube-public ")).To(HaveLen(2))
		})
	})

	Describe("IsExcluded", func() {
		It("returns false for empty patterns", func() {
			Expect(mirror.IsExcluded("kube-system", nil)).To(BeFalse())
		})

		It("returns false for empty namespace", func() {
			patterns := mirror.ParseGlobPatterns("kube-*")
			Expect(mirror.IsExcluded("", patterns)).To(BeFalse())
		})

		It("matches exact values", func() {
			patterns := mirror.ParseGlobPatterns("kube-system")
			Expect(mirror.IsExcluded("kube-system", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("default", patterns)).To(BeFalse())
		})

		It("supports star wildcards", func() {
			patterns := mirror.ParseGlobPatterns("ephie-*")
			Expect(mirror.IsExcluded("ephie-pr-123", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("prod-namespace", patterns)).To(BeFalse())
		})

		It("lets star match everything", func() {
			patterns := mirror.ParseGlobPatterns("*")
			Expect(mirror.IsExcluded("any-namespace", patterns)).To(BeTrue())
		})

		It("supports suffix wildcards", func() {
			patterns := mirror.ParseGlobPatterns("*-temp")
			Expect(mirror.IsExcluded("feature-temp", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("feature-prod", patterns)).To(BeFalse())
		})

		It("supports question-mark wildcards", func() {
			patterns := mirror.ParseGlobPatterns("ns-?")
			Expect(mirror.IsExcluded("ns-a", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("ns-1", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("ns-ab", patterns)).To(BeFalse())
		})

		It("supports multiple patterns", func() {
			patterns := mirror.ParseGlobPatterns("kube-system,kube-public")
			Expect(mirror.IsExcluded("kube-system", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("kube-public", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("default", patterns)).To(BeFalse())
		})

		It("keeps dots literal", func() {
			patterns := mirror.ParseGlobPatterns("ns.special")
			Expect(mirror.IsExcluded("ns.special", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("nsXspecial", patterns)).To(BeFalse())
		})

		It("keeps brackets literal", func() {
			patterns := mirror.ParseGlobPatterns("ns[1]")
			Expect(mirror.IsExcluded("ns[1]", patterns)).To(BeTrue())
			Expect(mirror.IsExcluded("ns1", patterns)).To(BeFalse())
		})
	})
})

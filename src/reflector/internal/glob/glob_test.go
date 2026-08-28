package glob_test

import (
	"testing"

	reflectorglob "github.com/emberstack/kubernetes-reflector/internal/glob"
)

func TestParsePatternsEmptyInput(t *testing.T) {
	cases := []string{"", "   "}
	for _, tc := range cases {
		if got := reflectorglob.ParsePatterns(tc); len(got) != 0 {
			t.Fatalf("expected empty patterns for %q, got %d", tc, len(got))
		}
	}
}

func TestParsePatternsSinglePattern(t *testing.T) {
	got := reflectorglob.ParsePatterns("kube-system")
	if len(got) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(got))
	}
}

func TestParsePatternsMultiplePatterns(t *testing.T) {
	got := reflectorglob.ParsePatterns("kube-system,kube-public,default")
	if len(got) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(got))
	}
}

func TestParsePatternsTrimAndIgnoreEmptyEntries(t *testing.T) {
	got := reflectorglob.ParsePatterns(" kube-system , , kube-public ")
	if len(got) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(got))
	}
}

func TestIsExcludedEmptyPatterns(t *testing.T) {
	if reflectorglob.IsExcluded("kube-system", nil) {
		t.Fatal("expected false for empty patterns")
	}
}

func TestIsExcludedEmptyNamespace(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("kube-*")
	if reflectorglob.IsExcluded("", patterns) {
		t.Fatal("expected false for empty namespace")
	}
}

func TestIsExcludedExactMatch(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("kube-system")
	if !reflectorglob.IsExcluded("kube-system", patterns) {
		t.Fatal("expected exact match")
	}
}

func TestIsExcludedNoMatch(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("kube-system")
	if reflectorglob.IsExcluded("default", patterns) {
		t.Fatal("expected false")
	}
}

func TestIsExcludedStarWildcard(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("ephie-*")
	if !reflectorglob.IsExcluded("ephie-pr-123", patterns) {
		t.Fatal("expected wildcard prefix match")
	}
	if reflectorglob.IsExcluded("prod-namespace", patterns) {
		t.Fatal("expected no match for different prefix")
	}
}

func TestIsExcludedStarMatchesAll(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("*")
	if !reflectorglob.IsExcluded("any-namespace", patterns) {
		t.Fatal("expected * to match everything")
	}
}

func TestIsExcludedSuffixWildcard(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("*-temp")
	if !reflectorglob.IsExcluded("feature-temp", patterns) {
		t.Fatal("expected suffix wildcard match")
	}
	if reflectorglob.IsExcluded("feature-prod", patterns) {
		t.Fatal("expected no suffix wildcard match")
	}
}

func TestIsExcludedQuestionWildcard(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("ns-?")
	if !reflectorglob.IsExcluded("ns-a", patterns) {
		t.Fatal("expected ns-a to match")
	}
	if !reflectorglob.IsExcluded("ns-1", patterns) {
		t.Fatal("expected ns-1 to match")
	}
	if reflectorglob.IsExcluded("ns-ab", patterns) {
		t.Fatal("expected ns-ab not to match")
	}
}

func TestIsExcludedMultiplePatterns(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("kube-system,kube-public")
	if !reflectorglob.IsExcluded("kube-system", patterns) {
		t.Fatal("expected kube-system to match")
	}
	if !reflectorglob.IsExcluded("kube-public", patterns) {
		t.Fatal("expected kube-public to match")
	}
	if reflectorglob.IsExcluded("default", patterns) {
		t.Fatal("expected default not to match")
	}
}

func TestIsExcludedLiteralDot(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("ns.special")
	if !reflectorglob.IsExcluded("ns.special", patterns) {
		t.Fatal("expected literal dot match")
	}
	if reflectorglob.IsExcluded("nsXspecial", patterns) {
		t.Fatal("expected dot to stay literal")
	}
}

func TestIsExcludedLiteralBrackets(t *testing.T) {
	patterns := reflectorglob.ParsePatterns("ns[1]")
	if !reflectorglob.IsExcluded("ns[1]", patterns) {
		t.Fatal("expected literal brackets match")
	}
	if reflectorglob.IsExcluded("ns1", patterns) {
		t.Fatal("expected brackets not to act like a character class")
	}
}

package helps

import (
	"strings"
	"testing"
)

// TestBuildSensitiveWordMatcherRespectsWordBoundaries verifies short sensitive
// words do not corrupt longer words containing them (H24d).
func TestBuildSensitiveWordMatcherRespectsWordBoundaries(t *testing.T) {
	m := BuildSensitiveWordMatcher([]string{"pass"})
	if m == nil {
		t.Fatal("matcher is nil")
	}
	obfuscated := m.obfuscateText("my password is pass")
	if strings.Contains(obfuscated, "p"+zeroWidthSpace+"assword") {
		t.Fatalf("substring match corrupted password: %q", obfuscated)
	}
	if !strings.Contains(obfuscated, "p"+zeroWidthSpace+"ass") {
		t.Fatalf("whole word not obfuscated: %q", obfuscated)
	}
}

// TestBuildSensitiveWordMatcherKeepsNonASCIISubstringMatching verifies words
// with non-word (e.g. Chinese) characters keep substring matching.
func TestBuildSensitiveWordMatcherKeepsNonASCIISubstringMatching(t *testing.T) {
	m := BuildSensitiveWordMatcher([]string{"密钥"})
	if m == nil {
		t.Fatal("matcher is nil")
	}
	obfuscated := m.obfuscateText("我的密钥在这里")
	if !strings.Contains(obfuscated, "密"+zeroWidthSpace+"钥") {
		t.Fatalf("Chinese sensitive word not obfuscated: %q", obfuscated)
	}
}

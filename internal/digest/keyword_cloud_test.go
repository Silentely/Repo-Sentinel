package digest

import (
	"testing"
)

func TestExtractTopEventKeywords(t *testing.T) {
	titles := []string{
		"Update actions/checkout action to v4",
		"Update actions/setup-node to v4",
		"Bump nanoid from 3.0 to 3.3",
		"Bump nanoid to 3.3.20",
		"Bump golang docker to 1.27",
	}

	top := ExtractTopEventKeywords(titles, 3)
	if len(top) == 0 {
		t.Fatal("expected keywords extracted")
	}

	// nanoid 应出现在高频关键词中
	foundNanoid := false
	for _, kw := range top {
		if kw == "nanoid" {
			foundNanoid = true
		}
	}
	if !foundNanoid {
		t.Errorf("expected nanoid in top keywords, got: %v", top)
	}
}

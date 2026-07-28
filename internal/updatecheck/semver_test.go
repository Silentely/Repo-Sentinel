package updatecheck

import "testing"

func TestNormalizeAndCompare(t *testing.T) {
	if got := NormalizeVersion("v1.2.3"); got != "1.2.3" {
		t.Fatalf("normalize=%q", got)
	}
	if !IsUpdateAvailable("0.3.1", "0.4.0") {
		t.Fatal("0.4.0 should be newer than 0.3.1")
	}
	if IsUpdateAvailable("0.4.0", "0.4.0") {
		t.Fatal("same version is not an update")
	}
	if IsUpdateAvailable("0.4.1", "0.4.0") {
		t.Fatal("older latest is not an update")
	}
	if !IsUpdateAvailable("0.3.1", "v0.3.2") {
		t.Fatal("v-prefix should compare")
	}
}

func TestParseSemverIgnoresPreRelease(t *testing.T) {
	maj, min, pat := ParseSemver("1.2.3-beta+build")
	if maj != 1 || min != 2 || pat != 3 {
		t.Fatalf("got %d.%d.%d", maj, min, pat)
	}
}

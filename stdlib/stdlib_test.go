package stdlib

import (
	"testing"
)

func TestCapabilityMetadataReturnsIndependentCopies(t *testing.T) {
	host := HostCapabilities()
	if len(host) == 0 {
		t.Fatal("embedded standard library has no host capability metadata")
	}
	host[0] = "changed"
	if HostCapabilities()[0] == "changed" {
		t.Fatal("HostCapabilities exposed mutable package state")
	}

	packages := PackageCapabilities()
	for path, capabilities := range packages {
		if len(capabilities) == 0 {
			continue
		}
		capabilities[0] = "changed"
		if PackageCapabilities()[path][0] == "changed" {
			t.Fatal("PackageCapabilities exposed a capability slice")
		}
		packages["example/changed"] = []string{"changed"}
		if _, ok := PackageCapabilities()["example/changed"]; ok {
			t.Fatal("PackageCapabilities exposed its package map")
		}
		return
	}
	t.Fatal("embedded standard library has no package capability metadata")
}

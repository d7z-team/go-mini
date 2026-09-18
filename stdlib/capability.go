package stdlib

import "slices"

// HostCapability identifies a host service used by standard-library source.
type HostCapability string

const (
	CapabilityConsole     HostCapability = "console"
	CapabilityEnvironment HostCapability = "environment"
	CapabilityFilesystem  HostCapability = "filesystem"
)

var packageCapabilities = map[string][]HostCapability{
	"fmt": {CapabilityConsole},
	"os":  {CapabilityEnvironment, CapabilityFilesystem},
}

// HostCapabilities returns the host services used by the embedded standard
// library. The returned slice may be modified by the caller.
func HostCapabilities() []HostCapability {
	seen := make(map[HostCapability]struct{})
	for _, capabilities := range packageCapabilities {
		for _, capability := range capabilities {
			seen[capability] = struct{}{}
		}
	}
	capabilities := make([]HostCapability, 0, len(seen))
	for capability := range seen {
		capabilities = append(capabilities, capability)
	}
	slices.Sort(capabilities)
	return capabilities
}

// PackageCapabilities returns optional host assembly hints for each source
// package. These are not compiler requirements. The caller may modify the result.
func PackageCapabilities() map[string][]string {
	out := make(map[string][]string, len(packageCapabilities))
	for path, capabilities := range packageCapabilities {
		out[path] = make([]string, len(capabilities))
		for i, capability := range capabilities {
			out[path][i] = string(capability)
		}
	}
	return out
}

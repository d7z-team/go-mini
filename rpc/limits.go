package rpc

// Limits bounds one RPC session. Zero fields select protocol defaults.
type Limits struct {
	MaxFrameBytes      int
	MaxMessageBytes    int
	MaxInFlightBytes   int
	MaxBindings        int
	MaxPendingCalls    int
	MaxPendingResults  int
	MaxPendingControls int
	MaxResources       int
	MaxMethods         int
	MaxValueDepth      int
	MaxValueElements   int
}

// NormalizeLimits fills zero-valued limits with bounded protocol defaults.
func NormalizeLimits(limits Limits) Limits { return normalizeLimits(limits) }

func normalizeLimits(limits Limits) Limits {
	if limits.MaxFrameBytes == 0 {
		limits.MaxFrameBytes = 1 << 20
	}
	if limits.MaxMessageBytes == 0 {
		limits.MaxMessageBytes = 64 << 20
	}
	if limits.MaxInFlightBytes == 0 {
		limits.MaxInFlightBytes = 128 << 20
	}
	if limits.MaxBindings == 0 {
		limits.MaxBindings = 4096
	}
	if limits.MaxPendingCalls == 0 {
		limits.MaxPendingCalls = 65_536
	}
	if limits.MaxPendingResults == 0 {
		limits.MaxPendingResults = 65_536
	}
	if limits.MaxPendingControls == 0 {
		limits.MaxPendingControls = 4096
	}
	if limits.MaxResources == 0 {
		limits.MaxResources = 65_536
	}
	if limits.MaxMethods == 0 {
		limits.MaxMethods = 4096
	}
	if limits.MaxValueDepth == 0 {
		limits.MaxValueDepth = 128
	}
	if limits.MaxValueElements == 0 {
		limits.MaxValueElements = 1_000_000
	}
	return limits
}

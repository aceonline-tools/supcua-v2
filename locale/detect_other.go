//go:build !windows

package locale

func platformDetector() Detector { return Detector{} }

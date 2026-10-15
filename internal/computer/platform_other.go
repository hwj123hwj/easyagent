//go:build !darwin

package computer

func platformSupported() bool { return false }

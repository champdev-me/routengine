//go:build !darwin

package confdir

func platformDir() (string, bool) { return "", false }

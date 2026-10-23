//go:build !windows

package update

import "errors"

func launchInstaller(string) error {
	return errors.New("installing updates by itself is only available on Windows")
}

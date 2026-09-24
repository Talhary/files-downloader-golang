//go:build !windows

package tunnel

func SetWindowsSystemProxy(socksPort int) error {
	return nil
}

func ClearWindowsSystemProxy() error {
	return nil
}

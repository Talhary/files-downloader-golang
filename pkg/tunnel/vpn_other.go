//go:build !windows

package tunnel

type VPNController struct{}

func NewVPNController() *VPNController {
	return &VPNController{}
}

func (vc *VPNController) Start(socksPort int, bugHost string, logFn func(string)) error {
	return nil
}

func (vc *VPNController) Stop(logFn func(string)) {}

func (vc *VPNController) IsActive() bool {
	return false
}

func IsAdmin() bool {
	return false
}

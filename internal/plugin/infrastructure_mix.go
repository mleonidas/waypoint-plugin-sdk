package plugin

import (
	"github.com/hashicorp/waypoint-plugin-sdk/component"
)

type mix_Infra_Authenticator struct {
	component.Authenticator
	component.ConfigurableNotify
	component.Infra
	component.Documented
}

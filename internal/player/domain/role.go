package domain

// Role is a player's preferred position. It deliberately does not depend on
// the generated Protobuf enum so the domain can be used by any transport.
type Role string

const (
	RoleVanguard Role = "vanguard"
	RoleRoamer   Role = "roamer"
	RoleCore     Role = "core"
	RoleRanged   Role = "ranged"
	RoleSupport  Role = "support"
)

func (r Role) Valid() bool {
	switch r {
	case RoleVanguard, RoleRoamer, RoleCore, RoleRanged, RoleSupport:
		return true
	default:
		return false
	}
}

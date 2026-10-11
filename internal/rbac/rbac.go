package rbac

import "context"

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type Permission string

const (
	PermissionRead    Permission = "read"
	PermissionOperate Permission = "operate"
	PermissionManage  Permission = "manage"
)

type Principal struct {
	UserID string
	Role   Role
}

type principalKey struct{}

func Allows(role Role, permission Permission) bool {
	switch role {
	case RoleViewer:
		return permission == PermissionRead
	case RoleOperator:
		return permission == PermissionRead || permission == PermissionOperate
	case RoleAdmin:
		return permission == PermissionRead || permission == PermissionOperate || permission == PermissionManage
	default:
		return false
	}
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

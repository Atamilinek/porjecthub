package rbac_test

import (
	"testing"

	"github.com/projecthub/projecthub/internal/rbac"
)

func TestRolePermissionsMatchViewerOperatorAdminMatrix(t *testing.T) {
	cases := []struct {
		role       rbac.Role
		permission rbac.Permission
		want       bool
	}{
		{rbac.RoleViewer, rbac.PermissionRead, true},
		{rbac.RoleViewer, rbac.PermissionOperate, false},
		{rbac.RoleViewer, rbac.PermissionManage, false},
		{rbac.RoleOperator, rbac.PermissionRead, true},
		{rbac.RoleOperator, rbac.PermissionOperate, true},
		{rbac.RoleOperator, rbac.PermissionManage, false},
		{rbac.RoleAdmin, rbac.PermissionRead, true},
		{rbac.RoleAdmin, rbac.PermissionOperate, true},
		{rbac.RoleAdmin, rbac.PermissionManage, true},
		{rbac.Role("unknown"), rbac.PermissionRead, false},
	}
	for _, tc := range cases {
		if got := rbac.Allows(tc.role, tc.permission); got != tc.want {
			t.Errorf("Allows(%q, %q) = %v, want %v", tc.role, tc.permission, got, tc.want)
		}
	}
}

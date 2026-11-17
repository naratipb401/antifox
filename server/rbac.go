package server

import (
	"net/http"
	"sort"
	"strings"

	"antifox/db"
	"golang.org/x/crypto/bcrypt"
)

// Platform permission management API (/api/rbac/*).

type rbacUserView struct {
	db.RBACUser
	RoleIDs []string `json:"role_ids"`
}

type rbacRoleView struct {
	db.RBACRole
	Permissions []string `json:"permissions"`
}

// GET /api/rbac/me — the logged-in user's identity + roles + permissions.
func (s *Server) rbacMe(w http.ResponseWriter, r *http.Request) {
	acc := CurrentAccess(r)
	if acc == nil {
		writeErr(w, 401, "invalid session")
		return
	}
	perms := make([]string, 0, len(acc.Permissions))
	for k, v := range acc.Permissions {
		if v {
			perms = append(perms, k)
		}
	}
	sort.Strings(perms)
	writeJSON(w, 200, map[string]any{
		"user":        acc.User,
		"roles":       acc.Roles,
		"permissions": perms,
		"scope":       acc.Scope,
	})
}

// GET /api/rbac/metadata — permission catalog + roles (for the admin UI).
func (s *Server) rbacMetadata(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	roles, err := pg.ListRBACRoles()
	if err != nil {
		writeErr(w, 500, "failed to load roles")
		return
	}
	writeJSON(w, 200, map[string]any{
		"permissions": db.RBACPermissions,
		"roles":       roles,
		"scopes":      []string{db.RBACScopeAll, db.RBACScopeAssigned, db.RBACScopeOwn},
	})
}

// GET /api/rbac/users
func (s *Server) rbacListUsers(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	users, err := pg.ListRBACUsers()
	if err != nil {
		writeErr(w, 500, "failed to load users")
		return
	}
	out := make([]rbacUserView, 0, len(users))
	for _, u := range users {
		ids, _ := pg.ListRBACUserRoleIDs(u.ID)
		if ids == nil {
			ids = []string{}
		}
		out = append(out, rbacUserView{RBACUser: u, RoleIDs: ids})
	}
	writeJSON(w, 200, map[string]any{"users": out})
}

// POST /api/rbac/users
func (s *Server) rbacCreateUser(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Username    string   `json:"username"`
		DisplayName string   `json:"display_name"`
		Password    string   `json:"password"`
		Enabled     *bool    `json:"enabled"`
		RoleIDs     []string `json:"role_ids"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid parameters")
		return
	}
	username := strings.TrimSpace(strings.ToLower(req.Username))
	if username == "" || len(req.Password) < 8 {
		writeErr(w, 400, "username required, password at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "password hashing failed")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	u, err := pg.CreateRBACUser(username, req.DisplayName, string(hash), enabled, req.RoleIDs)
	if err != nil {
		msg := "failed to create user"
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(err.Error(), "duplicate") {
			msg = "username already exists"
		}
		writeErr(w, 400, msg)
		return
	}
	pg.RecordAudit(auditActor(r), "rbac", "create_user", "success", clientIP(r), "created user "+username, map[string]any{"user_id": u.ID})
	writeJSON(w, 200, map[string]any{"user": u})
}

// PUT /api/rbac/users/{id}
func (s *Server) rbacUpdateUser(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErr(w, 400, "user id missing")
		return
	}
	var req struct {
		DisplayName string    `json:"display_name"`
		Enabled     *bool     `json:"enabled"`
		RoleIDs     *[]string `json:"role_ids"`
		Password    string    `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid parameters")
		return
	}
	if err := pg.UpdateRBACUser(id, req.DisplayName, req.Enabled, req.RoleIDs); err != nil {
		writeErr(w, 500, "failed to update user")
		return
	}
	if pw := strings.TrimSpace(req.Password); pw != "" {
		if len(pw) < 8 {
			writeErr(w, 400, "password at least 8 characters")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			writeErr(w, 500, "password hashing failed")
			return
		}
		if err := pg.UpdateRBACUserPassword(id, string(hash)); err != nil {
			writeErr(w, 500, "failed to reset password")
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// DELETE /api/rbac/users/{id}
func (s *Server) rbacDeleteUser(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := pg.DeleteRBACUser(id); err != nil {
		writeErr(w, 400, "delete failed: "+err.Error())
		return
	}
	pg.RecordAudit(auditActor(r), "rbac", "delete_user", "success", clientIP(r), "deleted user "+id, nil)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// GET /api/rbac/roles
func (s *Server) rbacListRoles(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	roles, err := pg.ListRBACRoles()
	if err != nil {
		writeErr(w, 500, "failed to load roles")
		return
	}
	out := make([]rbacRoleView, 0, len(roles))
	for _, r := range roles {
		keys, _ := pg.ListRBACRolePermissionKeys(r.ID)
		if keys == nil {
			keys = []string{}
		}
		out = append(out, rbacRoleView{RBACRole: r, Permissions: keys})
	}
	writeJSON(w, 200, map[string]any{"roles": out})
}

type roleReq struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Scope       string   `json:"scope"`
	Permissions []string `json:"permissions"`
}

// POST /api/rbac/roles
func (s *Server) rbacCreateRole(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req roleReq
	if err := decode(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, 400, "role name is required")
		return
	}
	role, err := pg.UpsertRBACRole("", req.Name, req.Description, req.Scope, req.Permissions)
	if err != nil {
		writeErr(w, 400, "failed to create role: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"role": role})
}

// PUT /api/rbac/roles/{id}
func (s *Server) rbacUpdateRole(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var req roleReq
	if err := decode(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, 400, "role name is required")
		return
	}
	role, err := pg.UpsertRBACRole(id, req.Name, req.Description, req.Scope, req.Permissions)
	if err != nil {
		writeErr(w, 400, "failed to update role: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"role": role})
}

// DELETE /api/rbac/roles/{id}
func (s *Server) rbacDeleteRole(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := pg.DeleteRBACRole(id); err != nil {
		writeErr(w, 400, "failed to delete role: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

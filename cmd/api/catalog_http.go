package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/projecthub/projecthub/internal/categories"
	"github.com/projecthub/projecthub/internal/projects"
	"github.com/projecthub/projecthub/internal/rbac"
)

type catalogHTTP struct {
	auth       *authHTTP
	projects   *projects.Service
	categories *categories.Service
}
type projectPayload struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Icon        string               `json:"icon"`
	CategoryID  string               `json:"category_id"`
	SortOrder   int                  `json:"sort_order"`
	Links       []projects.LinkInput `json:"links"`
}
type categoryPayload struct {
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sort_order"`
}

func (c *catalogHTTP) register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/projects", c.auth.require(rbac.PermissionRead, http.HandlerFunc(c.listProjects)))
	mux.Handle("POST /api/v1/projects", c.auth.require(rbac.PermissionManage, http.HandlerFunc(c.createProject)))
	mux.Handle("GET /api/v1/projects/{id}", c.auth.require(rbac.PermissionRead, http.HandlerFunc(c.getProject)))
	mux.Handle("PUT /api/v1/projects/{id}", c.auth.require(rbac.PermissionManage, http.HandlerFunc(c.updateProject)))
	mux.Handle("DELETE /api/v1/projects/{id}", c.auth.require(rbac.PermissionManage, http.HandlerFunc(c.deleteProject)))
	mux.Handle("PUT /api/v1/projects/{id}/favorite", c.auth.require(rbac.PermissionRead, http.HandlerFunc(c.favoriteProject)))
	mux.Handle("GET /api/v1/categories", c.auth.require(rbac.PermissionRead, http.HandlerFunc(c.listCategories)))
	mux.Handle("POST /api/v1/categories", c.auth.require(rbac.PermissionManage, http.HandlerFunc(c.createCategory)))
	mux.Handle("GET /api/v1/categories/{id}", c.auth.require(rbac.PermissionRead, http.HandlerFunc(c.getCategory)))
	mux.Handle("PUT /api/v1/categories/{id}", c.auth.require(rbac.PermissionManage, http.HandlerFunc(c.updateCategory)))
	mux.Handle("DELETE /api/v1/categories/{id}", c.auth.require(rbac.PermissionManage, http.HandlerFunc(c.deleteCategory)))
}
func (c *catalogHTTP) listProjects(w http.ResponseWriter, r *http.Request) {
	p := principalFromRequest(r)
	q := r.URL.Query()
	filter := projects.ListFilter{Query: q.Get("q"), CategoryID: q.Get("category_id"), FavoritesOnly: q.Get("favorites") == "true", SortBy: q.Get("sort"), SortDirection: q.Get("direction")}
	var err error
	if filter.Limit, err = queryInt(q.Get("limit"), 0); err != nil {
		badRequest(w)
		return
	}
	if filter.Offset, err = queryInt(q.Get("offset"), 0); err != nil {
		badRequest(w)
		return
	}
	items, err := c.projects.List(r.Context(), p.User.ID, filter)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}
func (c *catalogHTTP) getProject(w http.ResponseWriter, r *http.Request) {
	p := principalFromRequest(r)
	item, err := c.projects.Get(r.Context(), r.PathValue("id"), p.User.ID)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (c *catalogHTTP) createProject(w http.ResponseWriter, r *http.Request) {
	var body projectPayload
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	item, err := c.projects.Create(r.Context(), toProjectInput(body))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if err = c.auth.auditTarget(r, principalFromRequest(r).User.ID, "project", item.ID, "project.create", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (c *catalogHTTP) updateProject(w http.ResponseWriter, r *http.Request) {
	var body projectPayload
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	item, err := c.projects.Update(r.Context(), r.PathValue("id"), toProjectInput(body))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if err = c.auth.auditTarget(r, principalFromRequest(r).User.ID, "project", item.ID, "project.update", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (c *catalogHTTP) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := c.projects.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeCatalogError(w, err)
		return
	}
	if err := c.auth.auditTarget(r, principalFromRequest(r).User.ID, "project", r.PathValue("id"), "project.delete", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (c *catalogHTTP) favoriteProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Favorite bool `json:"favorite"`
	}
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	p := principalFromRequest(r)
	if err := c.projects.SetFavorite(r.Context(), p.User.ID, r.PathValue("id"), body.Favorite); err != nil {
		writeCatalogError(w, err)
		return
	}
	if err := c.auth.auditTarget(r, p.User.ID, "project", r.PathValue("id"), "project.favorite", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (c *catalogHTTP) listCategories(w http.ResponseWriter, r *http.Request) {
	items, err := c.categories.List(r.Context())
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}
func (c *catalogHTTP) getCategory(w http.ResponseWriter, r *http.Request) {
	item, err := c.categories.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (c *catalogHTTP) createCategory(w http.ResponseWriter, r *http.Request) {
	var body categoryPayload
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	item, err := c.categories.Create(r.Context(), categories.CreateInput{Name: body.Name, Icon: body.Icon, SortOrder: body.SortOrder})
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if err = c.auth.auditTarget(r, principalFromRequest(r).User.ID, "category", item.ID, "category.create", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (c *catalogHTTP) updateCategory(w http.ResponseWriter, r *http.Request) {
	var body categoryPayload
	if decodeJSON(w, r, &body) != nil {
		badRequest(w)
		return
	}
	item, err := c.categories.Update(r.Context(), r.PathValue("id"), categories.UpdateInput{Name: body.Name, Icon: body.Icon, SortOrder: body.SortOrder})
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	if err = c.auth.auditTarget(r, principalFromRequest(r).User.ID, "category", item.ID, "category.update", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (c *catalogHTTP) deleteCategory(w http.ResponseWriter, r *http.Request) {
	if err := c.categories.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeCatalogError(w, err)
		return
	}
	if err := c.auth.auditTarget(r, principalFromRequest(r).User.ID, "category", r.PathValue("id"), "category.delete", "success"); err != nil {
		http.Error(w, "audit unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func toProjectInput(p projectPayload) projects.CreateInput {
	return projects.CreateInput{Name: p.Name, Description: p.Description, Icon: p.Icon, CategoryID: p.CategoryID, SortOrder: p.SortOrder, Links: p.Links}
}
func queryInt(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
func badRequest(w http.ResponseWriter) {
	http.Error(w, "invalid request body or query", http.StatusBadRequest)
}
func writeCatalogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projects.ErrInvalidID), errors.Is(err, projects.ErrInvalidInput), errors.Is(err, categories.ErrInvalidID), errors.Is(err, categories.ErrInvalidInput):
		http.Error(w, "invalid input", http.StatusBadRequest)
	case errors.Is(err, projects.ErrNotFound), errors.Is(err, projects.ErrCategoryNotFound), errors.Is(err, categories.ErrNotFound):
		http.Error(w, "resource not found", http.StatusNotFound)
	case errors.Is(err, projects.ErrConflict), errors.Is(err, categories.ErrConflict):
		http.Error(w, "resource conflict", http.StatusConflict)
	default:
		if strings.Contains(err.Error(), "invalid") {
			http.Error(w, "invalid input", http.StatusBadRequest)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	}
}

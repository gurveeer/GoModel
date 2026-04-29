package admin

import (
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/echo/v5"

	"gomodel/internal/core"
	"gomodel/internal/modeloverrides"
	"gomodel/internal/providers"
)

type modelAccessResponse struct {
	Selector         string                   `json:"selector"`
	DefaultEnabled   bool                     `json:"default_enabled"`
	EffectiveEnabled bool                     `json:"effective_enabled"`
	UserPaths        []string                 `json:"user_paths,omitempty"`
	Override         *modeloverrides.Override `json:"override,omitempty"`
}

type modelInventoryResponse struct {
	providers.ModelWithProvider
	Access modelAccessResponse `json:"access"`
}

func (h *Handler) ListModels(c *echo.Context) error {
	if h.registry == nil {
		return c.JSON(http.StatusOK, []modelInventoryResponse{})
	}

	cat := core.ModelCategory(c.QueryParam("category"))
	if cat != "" && cat != core.CategoryAll {
		if !isValidCategory(cat) {
			return handleError(c, core.NewInvalidRequestError("invalid category: "+string(cat), nil))
		}
	}

	var models []providers.ModelWithProvider
	if cat != "" && cat != core.CategoryAll {
		models = h.registry.ListModelsWithProviderByCategory(cat)
	} else {
		models = h.registry.ListModelsWithProvider()
	}

	if models == nil {
		models = []providers.ModelWithProvider{}
	}
	if h.modelOverrides == nil {
		response := make([]modelInventoryResponse, 0, len(models))
		for _, model := range models {
			selector := core.ModelSelector{
				Provider: strings.TrimSpace(model.ProviderName),
				Model:    strings.TrimSpace(model.Model.ID),
			}
			response = append(response, modelInventoryResponse{
				ModelWithProvider: model,
				Access: modelAccessResponse{
					Selector:         selector.QualifiedModel(),
					DefaultEnabled:   true,
					EffectiveEnabled: true,
				},
			})
		}
		return c.JSON(http.StatusOK, response)
	}

	response := make([]modelInventoryResponse, 0, len(models))
	for _, model := range models {
		selector := core.ModelSelector{
			Provider: strings.TrimSpace(model.ProviderName),
			Model:    strings.TrimSpace(model.Model.ID),
		}
		effective := h.modelOverrides.EffectiveState(selector)
		access := modelAccessResponse{
			Selector:         effective.Selector,
			DefaultEnabled:   effective.DefaultEnabled,
			EffectiveEnabled: effective.Enabled,
			UserPaths:        append([]string(nil), effective.UserPaths...),
		}
		if override, ok := h.modelOverrides.Get(selector.QualifiedModel()); ok && override != nil {
			overrideCopy := *override
			access.Override = &overrideCopy
		}
		response = append(response, modelInventoryResponse{
			ModelWithProvider: model,
			Access:            access,
		})
	}

	return c.JSON(http.StatusOK, response)
}

// isValidCategory returns true if cat is a recognized model category.
func isValidCategory(cat core.ModelCategory) bool {
	return slices.Contains(core.AllCategories(), cat)
}

// ListCategories handles GET /admin/api/v1/models/categories
//
// @Summary      List model categories with counts
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   providers.CategoryCount
// @Failure      401  {object}  core.GatewayError
// @Router       /admin/api/v1/models/categories [get]
func (h *Handler) ListCategories(c *echo.Context) error {
	if h.registry == nil {
		return c.JSON(http.StatusOK, []providers.CategoryCount{})
	}

	return c.JSON(http.StatusOK, h.registry.GetCategoryCounts())
}

// DashboardConfig handles GET /admin/api/v1/dashboard/config
func (h *Handler) DashboardConfig(c *echo.Context) error {
	return c.JSON(http.StatusOK, cloneDashboardRuntimeConfig(h.runtimeConfig))
}

// ListBudgets handles GET /admin/api/v1/budgets.
// @Summary      List budgets with current status
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  budgetListResponse
// @Failure      401  {object}  core.GatewayError
// @Failure      503  {object}  core.GatewayError

package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/bejix/upstream-ops/backend/syncer"
	"github.com/gin-gonic/gin"
)

func registerUpstreamSync(g *gin.RouterGroup, d *Deps) {
	if d.UpstreamSync == nil {
		return
	}
	gp := g.Group("/upstream-sync")
	gp.GET("/targets", func(c *gin.Context) { listUpstreamSyncTargets(c, d) })
	gp.POST("/targets", func(c *gin.Context) { createUpstreamSyncTarget(c, d) })
	gp.PUT("/targets/:id", func(c *gin.Context) { updateUpstreamSyncTarget(c, d) })
	gp.DELETE("/targets/:id", func(c *gin.Context) { deleteUpstreamSyncTarget(c, d) })
	gp.POST("/targets/:id/check", func(c *gin.Context) { checkUpstreamSyncTarget(c, d) })
	gp.POST("/targets/:id/groups/sync", func(c *gin.Context) { syncUpstreamSyncTargetGroups(c, d) })
	gp.GET("/targets/:id/groups", func(c *gin.Context) { listUpstreamSyncTargetGroups(c, d) })
	gp.GET("/targets/:id/proxies", func(c *gin.Context) { listUpstreamSyncTargetProxies(c, d) })
	gp.GET("/overview", func(c *gin.Context) { getUpstreamSyncOverview(c, d) })
	gp.PUT("/accounts/:account_id/schedulable", func(c *gin.Context) { updateUpstreamSyncAccountSchedulable(c, d) })
	gp.PUT("/groups/:group_id/smart-routing", func(c *gin.Context) { updateUpstreamSyncGroupSmartRouting(c, d) })
	gp.GET("/source-models", func(c *gin.Context) { listUpstreamSyncSourceModels(c, d) })
	gp.GET("/sync-groups", func(c *gin.Context) { listUpstreamSyncGroups(c, d) })
	gp.POST("/sync-groups", func(c *gin.Context) { createUpstreamSyncGroup(c, d) })
	gp.PUT("/sync-groups/:id", func(c *gin.Context) { updateUpstreamSyncGroup(c, d) })
	gp.DELETE("/sync-groups/:id", func(c *gin.Context) { deleteUpstreamSyncGroup(c, d) })
	gp.POST("/sync-groups/:id/apply", func(c *gin.Context) { applyUpstreamSyncGroup(c, d) })
	gp.POST("/sync-groups/:id/delete-managed", func(c *gin.Context) { deleteUpstreamSyncManaged(c, d) })
	gp.GET("/sync-groups/:id/logs", func(c *gin.Context) { listUpstreamSyncGroupLogs(c, d) })
}

func updateUpstreamSyncGroupSmartRouting(c *gin.Context, d *Deps) {
	groupID, err := strconv.ParseInt(c.Param("group_id"), 10, 64)
	if err != nil || groupID <= 0 {
		if err == nil {
			err = errors.New("group_id must be greater than 0")
		}
		fail(c, http.StatusBadRequest, err)
		return
	}
	var raw struct {
		PrimaryPool  *[]syncer.SmartRoutingEntryInput `json:"primary_pool"`
		FallbackPool *[]syncer.SmartRoutingEntryInput `json:"fallback_pool"`
	}
	if err := c.ShouldBindJSON(&raw); err != nil || raw.PrimaryPool == nil || raw.FallbackPool == nil {
		if err == nil {
			err = errors.New("primary_pool and fallback_pool are required")
		}
		fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := d.UpstreamSync.UpdateOverviewGroupSmartRouting(c.Request.Context(), groupID, syncer.SmartRoutingUpdateInput{
		PrimaryPool: *raw.PrimaryPool, FallbackPool: *raw.FallbackPool,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, syncer.ErrInvalidOverviewRouting) {
			status = http.StatusBadRequest
		}
		fail(c, status, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func getUpstreamSyncOverview(c *gin.Context, d *Deps) {
	item, err := d.UpstreamSync.GetOverview(c.Request.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, syncer.ErrOverviewTargetNotConfigured) || errors.Is(err, syncer.ErrOverviewMultipleTargets) {
			status = http.StatusConflict
		}
		fail(c, status, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func updateUpstreamSyncAccountSchedulable(c *gin.Context, d *Deps) {
	accountID, err := strconv.ParseInt(c.Param("account_id"), 10, 64)
	if err != nil || accountID <= 0 {
		if err == nil {
			err = errors.New("account_id must be greater than 0")
		}
		fail(c, http.StatusBadRequest, err)
		return
	}
	var in struct {
		Schedulable *bool `json:"schedulable" binding:"required"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Schedulable == nil {
		if err == nil {
			err = errors.New("schedulable is required")
		}
		fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := d.UpstreamSync.SetOverviewAccountSchedulable(c.Request.Context(), accountID, *in.Schedulable)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func listUpstreamSyncTargets(c *gin.Context, d *Deps) {
	list, err := d.UpstreamSync.ListTargets()
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func createUpstreamSyncTarget(c *gin.Context, d *Deps) {
	var in syncer.TargetInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := d.UpstreamSync.CreateTarget(c.Request.Context(), in)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func updateUpstreamSyncTarget(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var in syncer.TargetInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := d.UpstreamSync.UpdateTarget(c.Request.Context(), id, in)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func deleteUpstreamSyncTarget(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := d.UpstreamSync.DeleteTarget(id); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func checkUpstreamSyncTarget(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := d.UpstreamSync.CheckTarget(c.Request.Context(), id); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func syncUpstreamSyncTargetGroups(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	list, err := d.UpstreamSync.SyncTargetGroups(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func listUpstreamSyncTargetGroups(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	includeMissing := c.Query("include_missing") == "1" || c.Query("include_missing") == "true"
	list, err := d.UpstreamSync.ListTargetGroups(id, includeMissing)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func listUpstreamSyncTargetProxies(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	list, err := d.UpstreamSync.ListTargetProxies(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func listUpstreamSyncSourceModels(c *gin.Context, d *Deps) {
	channelID, err := strconv.ParseUint(c.Query("channel_id"), 10, 64)
	if err != nil || channelID == 0 {
		if err == nil {
			err = errors.New("channel_id is required")
		}
		fail(c, http.StatusBadRequest, err)
		return
	}
	in := syncer.SourceModelsInput{
		ChannelID:       uint(channelID),
		SourceGroupName: c.Query("source_group_name"),
		Platform:        c.Query("platform"),
	}
	if raw := c.Query("sync_account_id"); raw != "" {
		id, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			fail(c, http.StatusBadRequest, parseErr)
			return
		}
		in.SyncAccountID = uint(id)
	}
	if raw := c.Query("source_group_id"); raw != "" {
		id, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			fail(c, http.StatusBadRequest, parseErr)
			return
		}
		in.SourceGroupID = &id
	}
	list, err := d.UpstreamSync.ListSourceModels(c.Request.Context(), in)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func listUpstreamSyncGroups(c *gin.Context, d *Deps) {
	list, err := d.UpstreamSync.ListSyncGroups()
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func createUpstreamSyncGroup(c *gin.Context, d *Deps) {
	var in syncer.SyncGroupDTO
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := d.UpstreamSync.CreateSyncGroup(in)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func updateUpstreamSyncGroup(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var in syncer.SyncGroupDTO
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := d.UpstreamSync.UpdateSyncGroup(id, in)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func deleteUpstreamSyncGroup(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := d.UpstreamSync.DeleteSyncGroup(id); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func applyUpstreamSyncGroup(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	log, err := d.UpstreamSync.ApplySyncGroup(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	if err := d.UpstreamSync.ReconcileSyncGroupRatePolicies(c.Request.Context(), id); err != nil && d.Log != nil {
		d.Log.Warn("reconcile rate policies after manual sync group apply", "syncGroupID", id, "err", err)
	}
	c.JSON(http.StatusOK, gin.H{"data": log})
}

func deleteUpstreamSyncManaged(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	log, err := d.UpstreamSync.DeleteManaged(c.Request.Context(), id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": log})
}

func listUpstreamSyncGroupLogs(c *gin.Context, d *Deps) {
	id, err := uintParam(c, "id")
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	page, pageSize, err := parsePageQuery(c)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	list, total, err := d.UpstreamSync.ListSyncGroupLogs(id, page, pageSize)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	pages := 1
	if total > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"items":     list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"pages":     pages,
	}})
}

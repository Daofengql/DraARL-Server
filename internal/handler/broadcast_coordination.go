package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"time"

	"draarl/internal/broadcast/model"
	"draarl/internal/broadcast/repository"
	broadcastruntime "draarl/internal/broadcast/runtime"
	schedruntime "draarl/internal/broadcast/scheduler/runtime"
	"draarl/internal/gormdb"
	"draarl/internal/routesync"
	"draarl/internal/udphub"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
)

func cancelBroadcastRunsAfterResourceMutation(c *gin.Context, runIDs []uint, code string) bool {
	if len(runIDs) == 0 {
		return true
	}
	ctx, cancel := broadcastMutationContext(c)
	defer cancel()
	_, err := broadcastruntime.CancelRunsAndWait(ctx, runIDs, &schedruntime.RunValidationError{Code: code})
	return writeBroadcastReleaseResult(c, err, "broadcast_resource_release", "资源已更新，但自动播报")
}

func cancelBroadcastGroupsAfterMutation(c *gin.Context, groupIDs []int, code string) bool {
	if len(groupIDs) == 0 {
		return true
	}
	ctx, cancel := broadcastMutationContext(c)
	defer cancel()
	_, err := broadcastruntime.CancelGroupsAndWait(ctx, groupIDs, &schedruntime.RunValidationError{Code: code})
	return writeBroadcastReleaseResult(c, err, "broadcast_group_release", "群组已更新，但自动播报")
}

func broadcastMutationContext(c *gin.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if c != nil && c.Request != nil {
		base = context.WithoutCancel(c.Request.Context())
	}
	return context.WithTimeout(base, broadcastOperationTimeout)
}

func writeBroadcastReleaseResult(c *gin.Context, err error, errorCodePrefix, messagePrefix string) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		writeBroadcastError(c, http.StatusServiceUnavailable, errorCodePrefix+"_timeout", messagePrefix+"尚未安全释放")
		return false
	}
	writeBroadcastError(c, http.StatusServiceUnavailable, errorCodePrefix+"_failed", messagePrefix+"释放失败")
	return false
}

func prepareEntityGroupBroadcastDeletion(c *gin.Context, userID, groupID int) ([]string, bool) {
	repo := repository.Default()
	audios, err := repo.ListAudios(c.Request.Context(), groupID)
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_group_assets_failed", "读取群组自动播报资源失败")
		return nil, false
	}
	objectKeys := make([]string, 0, len(audios)*2)
	for index := range audios {
		objectKeys = append(objectKeys, audios[index].OriginalObjectKey, audios[index].PlaybackObjectKey)
	}

	links, err := gormdb.NewGroupLinkRepository().GetLinksByTargetGroup(groupID)
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_group_topology_failed", "读取群组互联关系失败")
		return nil, false
	}
	sort.Slice(links, func(i, j int) bool { return links[i].LinkGroupID < links[j].LinkGroupID })
	for _, link := range links {
		policy, policyErr := repo.GetPolicy(c.Request.Context(), link.LinkGroupID)
		if policyErr != nil {
			writeVirtualGroupRepositoryError(c, policyErr)
			return nil, false
		}
		if policy.Mode == model.PolicyAllowSingleSource && policy.AllowedSourceGroupID != nil && *policy.AllowedSourceGroupID == groupID {
			mutation, updateErr := repo.UpdateVirtualGroupPolicy(c.Request.Context(), &model.VirtualGroupBroadcastPolicy{
				VirtualGroupID: link.LinkGroupID, Mode: model.PolicySuspendAll, UpdatedBy: userID,
			}, time.Now().UTC())
			if updateErr != nil {
				writeVirtualGroupRepositoryError(c, updateErr)
				return nil, false
			}
			if !waitForBroadcastTopologyMutation(c, mutation) {
				return nil, false
			}
		}
		mutation, removeErr := repo.RemoveVirtualGroupMember(c.Request.Context(), link.LinkGroupID, groupID, time.Now().UTC())
		if removeErr != nil {
			writeVirtualGroupRepositoryError(c, removeErr)
			return nil, false
		}
		if !waitForBroadcastTopologyMutation(c, mutation) {
			return nil, false
		}
	}
	if len(links) != 0 {
		udphub.RefreshGroupLinkCache()
		routesync.RefreshTopology()
	}
	return objectKeys, true
}

func cleanupDeletedBroadcastObjects(c *gin.Context, objectKeys []string) bool {
	cleanupPending := false
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = context.WithoutCancel(c.Request.Context())
	}
	for _, key := range objectKeys {
		if key == "" {
			continue
		}
		if err := storage.Delete(ctx, key); err != nil {
			cleanupPending = true
			log.Printf("[BROADCAST] cleanup deleted group audio object failed: %v", err)
		}
	}
	return cleanupPending
}

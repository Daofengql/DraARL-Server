package handler

import (
	"context"
	"net/http"
	"time"

	"draarl/internal/gormdb"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
)

const storageAuditTimeout = 2 * time.Minute

type storageAuditResponse struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Prefixes    []storage.AuditResult `json:"prefixes"`
	Totals      storageAuditTotals    `json:"totals"`
}

type storageAuditTotals struct {
	ScannedObjects      int64 `json:"scanned_objects"`
	ScannedBytes        int64 `json:"scanned_bytes"`
	ReferencedObjects   int64 `json:"referenced_objects"`
	ReferencedBytes     int64 `json:"referenced_bytes"`
	UnreferencedObjects int64 `json:"unreferenced_objects"`
	UnreferencedBytes   int64 `json:"unreferenced_bytes"`
	MissingReferences   int64 `json:"missing_references"`
}

// AuditStorage performs a read-only audit of immutable distribution objects.
// No cleanup is attempted, even when unreferenced objects are found.
func AuditStorage(c *gin.Context) {
	if _, ok := requireClientResourceAdmin(c); !ok {
		return
	}
	if !storage.IsEnabled() {
		writeClientResourceError(c, http.StatusServiceUnavailable, "存储服务不可用")
		return
	}
	references, err := gormdb.ManagedStorageReferences()
	if err != nil {
		writeClientResourceError(c, http.StatusInternalServerError, "读取存储引用失败")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), storageAuditTimeout)
	defer cancel()
	response := storageAuditResponse{GeneratedAt: time.Now().UTC(), Prefixes: make([]storage.AuditResult, 0, 3)}
	for _, prefix := range []string{"client-resources/", "firmware/", "broadcast-audios/"} {
		result, auditErr := storage.AuditPrefix(ctx, prefix, references[prefix])
		if auditErr != nil {
			writeClientResourceError(c, http.StatusInternalServerError, "扫描存储对象失败")
			return
		}
		response.Prefixes = append(response.Prefixes, result)
		response.Totals.ScannedObjects += result.ScannedObjects
		response.Totals.ScannedBytes += result.ScannedBytes
		response.Totals.ReferencedObjects += result.ReferencedObjects
		response.Totals.ReferencedBytes += result.ReferencedBytes
		for _, object := range result.UnreferencedObjects {
			response.Totals.UnreferencedObjects++
			response.Totals.UnreferencedBytes += object.Size
		}
		response.Totals.MissingReferences += int64(len(result.MissingReferences))
	}
	writeClientResourceSuccess(c, "存储审计完成（只读）", response)
}

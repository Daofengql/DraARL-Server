package middleware

import (
	"context"

	gormdb "draarl/internal/gormdb"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
)

// loadUserByName 优先使用两级用户缓存加载用户，降低认证/授权路径的每请求
// DB 负载；缓存不可用或未命中时回退直查数据库。
// 【H8 性能修复】用户信息有 2 分钟 TTL，角色变更会主动失效缓存，可接受。
func loadUserByName(ctx context.Context, name string) (*gormdb.User, error) {
	if userCache := cache.GetUserCache(); userCache != nil {
		return userCache.GetUserByName(ctx, name)
	}
	return gormdb.NewUserRepository().GetUserByNameContext(ctx, name)
}

func loadUserByID(ctx context.Context, id int) (*gormdb.User, error) {
	// Authentication needs the current version even if another process changed
	// the account. Profile caches are local and cannot guarantee revocation.
	return gormdb.NewUserRepository().GetUserByIDContext(ctx, id)
}

// userFromContext 返回 AuthMiddleware 已放入 context 的用户，未设置返回 nil。
func userFromContext(c *gin.Context) *gormdb.User {
	if c == nil {
		return nil
	}
	value, ok := c.Get("user")
	if !ok {
		return nil
	}
	user, ok := value.(*gormdb.User)
	if !ok || user == nil {
		return nil
	}
	return user
}

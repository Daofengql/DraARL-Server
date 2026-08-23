package handler

import (
	"log"
	"time"

	authstore "draarl/internal/auth"
)

// revokeUserRefreshSessions invalidates every refresh session after a password
// change. The password update has already committed, so a storage failure is
// logged for operational recovery instead of pretending the password update
// failed and encouraging a retry that could create another session.
func revokeUserRefreshSessions(userID int, reason string) {
	if userID <= 0 {
		return
	}
	if err := authstore.GetRefreshTokenStore().RevokeAllByUser(userID, reason, time.Now()); err != nil {
		log.Printf("吊销用户 refresh token 失败: user_id=%d reason=%s err=%v", userID, reason, err)
	}
}

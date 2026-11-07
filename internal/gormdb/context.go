package gormdb

import "context"

// WithContext returns a request-scoped repository without changing the original
// session. Transactions and cache-miss queries inherit cancellation and deadlines.
func (r *DeviceRepository) WithContext(ctx context.Context) *DeviceRepository {
	return &DeviceRepository{db: r.db.WithContext(ctx)}
}

func (r *UserRepository) WithContext(ctx context.Context) *UserRepository {
	return &UserRepository{db: r.db.WithContext(ctx)}
}

func (r *GroupRepository) WithContext(ctx context.Context) *GroupRepository {
	return &GroupRepository{db: r.db.WithContext(ctx)}
}

func (r *GroupMemberRepository) WithContext(ctx context.Context) *GroupMemberRepository {
	return &GroupMemberRepository{db: r.db.WithContext(ctx)}
}

func (r *GroupLinkRepository) WithContext(ctx context.Context) *GroupLinkRepository {
	return &GroupLinkRepository{db: r.db.WithContext(ctx)}
}

func (r *ServerRepository) WithContext(ctx context.Context) *ServerRepository {
	return &ServerRepository{db: r.db.WithContext(ctx)}
}

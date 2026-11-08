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

func (r *AssetRepository) WithContext(ctx context.Context) *AssetRepository {
	return &AssetRepository{db: r.db.WithContext(ctx)}
}

func (r *CenterPeerInviteRepository) WithContext(ctx context.Context) *CenterPeerInviteRepository {
	return &CenterPeerInviteRepository{db: r.db.WithContext(ctx)}
}

func (r *ClientResourceRepository) WithContext(ctx context.Context) *ClientResourceRepository {
	return &ClientResourceRepository{db: r.db.WithContext(ctx)}
}

func (r *DeviceConfigRepository) WithContext(ctx context.Context) *DeviceConfigRepository {
	return &DeviceConfigRepository{db: r.db.WithContext(ctx)}
}

func (r *FirmwareRepository) WithContext(ctx context.Context) *FirmwareRepository {
	return &FirmwareRepository{db: r.db.WithContext(ctx)}
}

func (r *GhostClientPreferenceRepository) WithContext(ctx context.Context) *GhostClientPreferenceRepository {
	return &GhostClientPreferenceRepository{db: r.db.WithContext(ctx)}
}

func (r *InterCenterLinkRepository) WithContext(ctx context.Context) *InterCenterLinkRepository {
	return &InterCenterLinkRepository{db: r.db.WithContext(ctx)}
}

func (r *LogbookRepository) WithContext(ctx context.Context) *LogbookRepository {
	return &LogbookRepository{db: r.db.WithContext(ctx)}
}

func (r *MessageRepository) WithContext(ctx context.Context) *MessageRepository {
	return &MessageRepository{db: r.db.WithContext(ctx)}
}

func (r *OperatorCertRepository) WithContext(ctx context.Context) *OperatorCertRepository {
	return &OperatorCertRepository{db: r.db.WithContext(ctx)}
}

func (r *RelayRepository) WithContext(ctx context.Context) *RelayRepository {
	return &RelayRepository{db: r.db.WithContext(ctx)}
}

func (r *OperatorLogRepository) WithContext(ctx context.Context) *OperatorLogRepository {
	return &OperatorLogRepository{db: r.db.WithContext(ctx)}
}

func (r *SiteConfigRepository) WithContext(ctx context.Context) *SiteConfigRepository {
	return &SiteConfigRepository{db: r.db.WithContext(ctx)}
}

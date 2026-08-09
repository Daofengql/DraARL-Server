package model

import "time"

const (
	AudioStatusProcessing = "processing"
	AudioStatusReady      = "ready"
	AudioStatusFailed     = "failed"

	ScheduleTypeOnce   = "once"
	ScheduleTypeDaily  = "daily"
	ScheduleTypeWeekly = "weekly"

	SuspendReasonActiveVirtualGroup = "active_virtual_group"

	PolicySuspendAll        = "suspend_all"
	PolicyAllowSingleSource = "allow_single_source"

	RunStatusClaimed                      = "claimed"
	RunStatusPlaying                      = "playing"
	RunStatusSucceeded                    = "succeeded"
	RunStatusSkippedRecentVoice           = "skipped_recent_voice"
	RunStatusSkippedDomainBusy            = "skipped_domain_busy"
	RunStatusSkippedInterconnected        = "skipped_interconnected"
	RunStatusSkippedNoReceiver            = "skipped_no_receiver"
	RunStatusSkippedSiteDisabled          = "skipped_site_disabled"
	RunStatusCancelled                    = "cancelled"
	RunStatusCancelledSiteDisabled        = "cancelled_site_disabled"
	RunStatusCancelledInterconnectEnabled = "cancelled_interconnect_enabled"
	RunStatusFailed                       = "failed"
)

// GroupReference and UserReference let the broadcast tables keep database
// foreign keys without coupling the broadcast domain to gormdb's repositories.
type GroupReference struct {
	ID int `gorm:"primaryKey;column:id"`
}

func (GroupReference) TableName() string { return "public_groups" }

type UserReference struct {
	ID int `gorm:"primaryKey;column:id"`
}

func (UserReference) TableName() string { return "users" }

type BroadcastAudio struct {
	ID                uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID           int       `gorm:"not null;index;column:group_id" json:"group_id"`
	Name              string    `gorm:"type:varchar(255);not null;column:name" json:"name"`
	OriginalObjectKey string    `gorm:"type:varchar(512);not null;column:original_object_key" json:"-"`
	PlaybackObjectKey string    `gorm:"type:varchar(512);column:playback_object_key" json:"-"`
	OriginalMIMEType  string    `gorm:"type:varchar(100);column:original_mime_type" json:"original_mime_type"`
	OriginalSize      int64     `gorm:"type:bigint;column:original_size" json:"original_size"`
	PlaybackSize      int64     `gorm:"type:bigint;column:playback_size" json:"playback_size"`
	DurationMS        int       `gorm:"type:int;column:duration_ms" json:"duration_ms"`
	PacketCount       int       `gorm:"type:int;column:packet_count" json:"packet_count"`
	SHA256            string    `gorm:"type:char(64);index:idx_broadcast_audio_group_sha,priority:2;column:sha256" json:"sha256"`
	Status            string    `gorm:"type:varchar(20);not null;default:processing;index;column:status" json:"status"`
	ErrorMessage      string    `gorm:"type:varchar(500);column:error_message" json:"error_message,omitempty"`
	CreatedBy         int       `gorm:"not null;column:created_by" json:"created_by"`
	CreatedAt         time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt         time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	Group   *GroupReference `gorm:"foreignKey:GroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	Creator *UserReference  `gorm:"foreignKey:CreatedBy;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (BroadcastAudio) TableName() string { return "broadcast_audios" }

type BroadcastSchedule struct {
	ID                        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID                   int        `gorm:"not null;index;column:group_id" json:"group_id"`
	AudioID                   uint       `gorm:"not null;index;column:audio_id" json:"audio_id"`
	Name                      string     `gorm:"type:varchar(255);not null;column:name" json:"name"`
	ScheduleType              string     `gorm:"type:varchar(16);not null;column:schedule_type" json:"schedule_type"`
	Timezone                  string     `gorm:"type:varchar(64);not null;column:timezone" json:"timezone"`
	ScheduledAt               *time.Time `gorm:"type:datetime(3);column:scheduled_at" json:"scheduled_at,omitempty"`
	LocalTime                 string     `gorm:"type:char(8);column:local_time" json:"local_time,omitempty"`
	WeekdayMask               uint8      `gorm:"type:tinyint unsigned;column:weekday_mask" json:"weekday_mask,omitempty"`
	NextRunAt                 *time.Time `gorm:"type:datetime(3);index:idx_broadcast_schedule_due,priority:2;column:next_run_at" json:"next_run_at,omitempty"`
	Enabled                   bool       `gorm:"type:tinyint(1);not null;index:idx_broadcast_schedule_due,priority:1;column:enabled" json:"enabled"`
	SuspendedReason           string     `gorm:"type:varchar(64);column:suspended_reason" json:"suspended_reason,omitempty"`
	SuspendedByVirtualGroupID *int       `gorm:"index;column:suspended_by_virtual_group_id" json:"suspended_by_virtual_group_id,omitempty"`
	SuspendedAt               *time.Time `gorm:"type:datetime(3);column:suspended_at" json:"suspended_at,omitempty"`
	CreatedBy                 int        `gorm:"not null;column:created_by" json:"created_by"`
	UpdatedBy                 int        `gorm:"not null;column:updated_by" json:"updated_by"`
	CreatedAt                 time.Time  `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt                 time.Time  `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	Group                   *GroupReference `gorm:"foreignKey:GroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	Audio                   *BroadcastAudio `gorm:"foreignKey:AudioID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Creator                 *UserReference  `gorm:"foreignKey:CreatedBy;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Updater                 *UserReference  `gorm:"foreignKey:UpdatedBy;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SuspendedByVirtualGroup *GroupReference `gorm:"foreignKey:SuspendedByVirtualGroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"-"`
}

func (BroadcastSchedule) TableName() string { return "broadcast_schedules" }

type VirtualGroupBroadcastPolicy struct {
	VirtualGroupID       int       `gorm:"primaryKey;column:virtual_group_id" json:"virtual_group_id"`
	Mode                 string    `gorm:"type:varchar(32);not null;default:suspend_all;column:mode" json:"mode"`
	AllowedSourceGroupID *int      `gorm:"index;column:allowed_source_group_id" json:"allowed_source_group_id,omitempty"`
	UpdatedBy            int       `gorm:"not null;column:updated_by" json:"updated_by"`
	CreatedAt            time.Time `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	VirtualGroup  *GroupReference `gorm:"foreignKey:VirtualGroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	AllowedSource *GroupReference `gorm:"foreignKey:AllowedSourceGroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Updater       *UserReference  `gorm:"foreignKey:UpdatedBy;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (VirtualGroupBroadcastPolicy) TableName() string {
	return "virtual_group_broadcast_policies"
}

type BroadcastRun struct {
	ID               uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ScheduleID       uint       `gorm:"not null;uniqueIndex:uk_broadcast_run_occurrence,priority:1;index;column:schedule_id" json:"schedule_id"`
	AudioID          uint       `gorm:"not null;index;column:audio_id" json:"audio_id"`
	SourceGroupID    int        `gorm:"not null;index;column:source_group_id" json:"source_group_id"`
	ScheduledFor     time.Time  `gorm:"type:datetime(3);not null;uniqueIndex:uk_broadcast_run_occurrence,priority:2;column:scheduled_for" json:"scheduled_for"`
	DomainKey        string     `gorm:"type:varchar(255);column:domain_key" json:"domain_key"`
	DomainGroupIDs   []int      `gorm:"serializer:json;type:json;column:domain_group_ids" json:"domain_group_ids"`
	Status           string     `gorm:"type:varchar(48);not null;index;column:status" json:"status"`
	LastVoiceAt      *time.Time `gorm:"type:datetime(3);column:last_voice_at" json:"last_voice_at,omitempty"`
	StartedAt        *time.Time `gorm:"type:datetime(3);column:started_at" json:"started_at,omitempty"`
	EndedAt          *time.Time `gorm:"type:datetime(3);column:ended_at" json:"ended_at,omitempty"`
	PlayedDurationMS int        `gorm:"type:int;column:played_duration_ms" json:"played_duration_ms"`
	SentPackets      int        `gorm:"type:int;column:sent_packets" json:"sent_packets"`
	DroppedPackets   int        `gorm:"type:int;column:dropped_packets" json:"dropped_packets"`
	ClaimedBy        string     `gorm:"type:varchar(128);column:claimed_by" json:"claimed_by,omitempty"`
	LeaseUntil       *time.Time `gorm:"type:datetime(3);index;column:lease_until" json:"lease_until,omitempty"`
	ErrorCode        string     `gorm:"type:varchar(64);column:error_code" json:"error_code,omitempty"`
	ErrorMessage     string     `gorm:"type:varchar(500);column:error_message" json:"error_message,omitempty"`
	CreatedAt        time.Time  `gorm:"autoCreateTime;column:created_at" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"autoUpdateTime;column:updated_at" json:"updated_at"`

	Schedule    *BroadcastSchedule `gorm:"foreignKey:ScheduleID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	Audio       *BroadcastAudio    `gorm:"foreignKey:AudioID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SourceGroup *GroupReference    `gorm:"foreignKey:SourceGroupID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

func (BroadcastRun) TableName() string { return "broadcast_runs" }

func IsScheduleType(value string) bool {
	switch value {
	case ScheduleTypeOnce, ScheduleTypeDaily, ScheduleTypeWeekly:
		return true
	default:
		return false
	}
}

func IsPolicyMode(value string) bool {
	return value == PolicySuspendAll || value == PolicyAllowSingleSource
}

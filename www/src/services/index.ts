// 导出所有服务
export { apiClient } from './api'
export { authService, ssoService, captchaService, emailAuthService, deviceBindService } from './auth'
export { approvalService } from './approval'
export { deviceService } from './device'
export { groupService } from './group'
export { userService } from './user'
export { relayService } from './relay'
export { serverService, edgeNodeService } from './server'
export type { EdgeNode, EdgeNodeUpdate, EdgeNodeCredentialResult } from './server'
export { logService } from './log'
export { platformService } from './platform'
export { commStatsService } from './commStats'
export { radioSessionService } from './radioSession'
export type { AdminRadioSession, GhostTransport } from './radioSession'
export { groupLinkService } from './groupLink'
export { broadcastService } from './broadcast'
export type {
  BroadcastAudio, BroadcastAudioStatus, BroadcastContext, BroadcastRun, BroadcastRunStatus,
  BroadcastSchedule, BroadcastScheduleInput, BroadcastScheduleType,
} from './broadcast'
export { listFirmware, uploadFirmware, deleteFirmware, getLatestFirmware } from './firmware'
export type { FirmwareRelease } from './firmware'
export {
  listClientResources, getClientResource, createClientResource, updateClientResource, deleteClientResource,
  listClientResourceStaging, retryClientResourceStaging, auditClientResourceStorage,
  listClientResourceReleases, getClientResourceRelease, createClientResourceRelease,
  completeClientResourceArtifact, publishClientResourceRelease,
  deleteClientResourceRelease,
} from './clientResource'
export type {
  ClientResource, ClientResourceRelease, ClientResourceArtifact,
  ClientResourceArtifactTarget, ClientResourceReleaseStatus, ClientResourceChannel,
  ClientResourceStagingItem, ClientResourceStagingListResult, ClientResourceStagingRetryResult,
  ClientResourceStorageAuditResult, ClientResourceStorageAuditResponse, ClientResourceDeleteResult,
  ClientResourceReleaseDeleteResult,
} from './clientResource'

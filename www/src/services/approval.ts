import { apiClient } from './api'
import type { PendingApproval, ApprovalRequest, ListResponse, CertificateApproval } from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
}

export const approvalService = {
  // 获取待审批用户列表
  async getPendingApprovals(page: number = 1, limit: number = 20, status: number = 0): Promise<ListResponse<PendingApproval>> {
    const params: Record<string, string | number> = { page, limit }
    if (status !== 0) {
      params.status = status
    }
    const res = await apiClient.get<BackendResponse<ListResponse<PendingApproval>>>(
      '/api/approvals/pending',
      { params }
    )
    return res.data!
  },

  // 获取操作证审批列表（按上传记录列出）
  async getCertificateApprovals(page: number = 1, limit: number = 20, status: number = 0): Promise<ListResponse<CertificateApproval>> {
    const params: Record<string, string | number> = { page, limit }
    if (status >= 0) {
      params.status = status
    }
    const res = await apiClient.get<BackendResponse<ListResponse<CertificateApproval>>>(
      '/api/certificate-approvals',
      { params }
    )
    return res.data!
  },

  // 审批用户（通过或拒绝）
  async approveUser(id: number, data: ApprovalRequest): Promise<void> {
    await apiClient.put(`/api/approvals/${id}/approve`, data)
  },

  // 审批操作证（通过或拒绝）
  async approveCertificate(id: number, data: ApprovalRequest): Promise<void> {
    await apiClient.put(`/api/operator-certificates/${id}/approve`, data)
  },
}

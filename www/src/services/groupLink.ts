import { apiClient } from './api'
import type { VirtualGroup, GroupLinkTarget, Group, ListResponse } from '../types'

interface BackendResponse<T> {
  code: number
  message: string
  data?: T
  warning?: string
}

// 虚拟互联组创建请求
interface CreateVirtualGroupRequest {
  name: string
  note?: string
  status?: number
}

// 虚拟互联组更新请求
interface UpdateVirtualGroupRequest {
  name?: string
  note?: string
  status?: number
}

// 添加关联群组请求
interface AddGroupLinkTargetRequest {
  target_group_id: number
}

export const groupLinkService = {
  // 获取所有虚拟互联组列表
  async getVirtualGroups(): Promise<VirtualGroup[]> {
    const res = await apiClient.get<BackendResponse<{ items: VirtualGroup[] }>>('/api/group-links')
    if (res.code !== 200) {
      throw new Error(res.message || '获取虚拟互联组列表失败')
    }
    return res.data?.items || []
  },

  // 获取可用的目标群组列表（非虚拟的公开群组）
  async getAvailableTargetGroups(): Promise<Group[]> {
    const res = await apiClient.get<BackendResponse<{ items: Group[] }>>('/api/group-links/available-targets')
    if (res.code !== 200) {
      throw new Error(res.message || '获取可用目标群组失败')
    }
    return res.data?.items || []
  },

  // 获取单个虚拟互联组详情
  async getVirtualGroup(id: number): Promise<VirtualGroup> {
    const res = await apiClient.get<BackendResponse<VirtualGroup>>(`/api/group-links/${id}`)
    if (res.code !== 200) {
      throw new Error(res.message || '获取虚拟互联组详情失败')
    }
    return res.data!
  },

  // 创建虚拟互联组
  async createVirtualGroup(data: CreateVirtualGroupRequest): Promise<VirtualGroup> {
    const res = await apiClient.post<BackendResponse<VirtualGroup>>('/api/group-links', data)
    if (res.code !== 201 && res.code !== 200) {
      throw new Error(res.message || '创建虚拟互联组失败')
    }
    return res.data!
  },

  // 更新虚拟互联组
  async updateVirtualGroup(id: number, data: UpdateVirtualGroupRequest): Promise<VirtualGroup> {
    const res = await apiClient.put<BackendResponse<VirtualGroup>>(`/api/group-links/${id}`, data)
    if (res.code !== 200) {
      throw new Error(res.message || '更新虚拟互联组失败')
    }
    return res.data!
  },

  // 删除虚拟互联组
  async deleteVirtualGroup(id: number): Promise<void> {
    const res = await apiClient.delete<BackendResponse<unknown>>(`/api/group-links/${id}`)
    if (res.code !== 200) {
      throw new Error(res.message || '删除虚拟互联组失败')
    }
  },

  // 获取互联组关联的目标群组列表
  async getGroupLinkTargets(id: number): Promise<GroupLinkTarget[]> {
    const res = await apiClient.get<BackendResponse<{ items: GroupLinkTarget[] }>>(`/api/group-links/${id}/targets`)
    if (res.code !== 200) {
      throw new Error(res.message || '获取关联群组列表失败')
    }
    return res.data?.items || []
  },

  // 添加关联群组
  async addGroupLinkTarget(id: number, targetGroupId: number): Promise<{ target_count: number; warning?: string }> {
    const res = await apiClient.post<BackendResponse<{ target_count: number }>>(`/api/group-links/${id}/targets`, {
      target_group_id: targetGroupId,
    })
    if (res.code !== 200) {
      throw new Error(res.message || '添加关联群组失败')
    }
    return {
      target_count: res.data?.target_count || 0,
      warning: res.warning,
    }
  },

  // 移除关联群组
  async removeGroupLinkTarget(id: number, targetId: number): Promise<void> {
    const res = await apiClient.delete<BackendResponse<unknown>>(`/api/group-links/${id}/targets/${targetId}`)
    if (res.code !== 200) {
      throw new Error(res.message || '移除关联群组失败')
    }
  },
}

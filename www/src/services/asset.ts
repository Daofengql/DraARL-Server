import { apiClient } from './api';

export interface Asset {
  id: number;
  parent_id: number | null;
  name: string;
  type: 'folder' | 'file';
  path?: string;
  size: number;
  mime_type?: string;
  remark?: string;
  sort_order: number;
  file_count?: number;
  folder_count?: number;
  download_url?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateFolderRequest {
  name: string;
  parent_id?: number | null;
  remark?: string;
}

export interface UpdateAssetRequest {
  name?: string;
  remark?: string;
  sort_order?: number;
}

export interface MoveAssetRequest {
  target_parent_id?: number | null;
}

export interface FolderFilesResponse {
  folder: Asset;
  files: Asset[];
}

export interface AssetTreeItem {
  id: number;
  name: string;
  type: string;
  remark?: string;
  sort_order: number;
  file_count?: number;
  created_at: string;
  updated_at: string;
}

// 获取资源列表（管理员���
export const getAssets = async (parentId?: number | null): Promise<Asset[]> => {
  const params = new URLSearchParams();
  if (parentId !== undefined && parentId !== null) {
    params.append('parent_id', parentId.toString());
  }
  const queryString = params.toString();
  const url = queryString ? `/api/assets?${queryString}` : '/api/assets';
  const res = await apiClient.get<{ code: number; message: string; data: Asset[] }>(url);
  if (res.code !== 200) {
    throw new Error(res.message || '获取资源列表失败');
  }
  return res.data || [];
};

// 创建文件夹
export const createFolder = async (data: CreateFolderRequest): Promise<Asset> => {
  const res = await apiClient.post<{ code: number; message: string; data: Asset }>('/api/assets/folder', data);
  if (res.code !== 200) {
    throw new Error(res.message || '创建文件夹失败');
  }
  return res.data;
};

// 上传文件
export const uploadFile = async (file: File, parentId: number, name?: string, remark?: string): Promise<Asset> => {
  const formData = new FormData();
  formData.append('file', file);
  formData.append('parent_id', parentId.toString());
  if (name) {
    formData.append('name', name);
  }
  if (remark) {
    formData.append('remark', remark);
  }

  const res = await apiClient.postFormData<{ code: number; message: string; data: Asset }>(
    '/api/assets/upload',
    formData
  );
  if (res.code !== 200) {
    throw new Error(res.message || '上传文件失败');
  }
  return res.data;
};

// 更新资源
export const updateAsset = async (id: number, data: UpdateAssetRequest): Promise<void> => {
  const res = await apiClient.put<{ code: number; message: string }>(`/api/assets/${id}`, data);
  if (res.code !== 200) {
    throw new Error(res.message || '更新资源失败');
  }
};

// 移动资源
export const moveAsset = async (id: number, data: MoveAssetRequest): Promise<void> => {
  const res = await apiClient.put<{ code: number; message: string }>(`/api/assets/${id}/move`, data);
  if (res.code !== 200) {
    throw new Error(res.message || '移动资源失败');
  }
};

// 覆盖文件
export const replaceFile = async (id: number, file: File): Promise<Asset> => {
  const formData = new FormData();
  formData.append('file', file);

  const res = await apiClient.postFormData<{ code: number; message: string; data: Asset }>(
    `/api/assets/${id}/replace`,
    formData
  );
  if (res.code !== 200) {
    throw new Error(res.message || '覆盖文件失败');
  }
  return res.data;
};

// 删除资源
export const deleteAsset = async (id: number): Promise<void> => {
  const res = await apiClient.delete<{ code: number; message: string }>(`/api/assets/${id}`);
  if (res.code !== 200) {
    throw new Error(res.message || '删除资源失败');
  }
};

// 获取资源目录树（公开接口）
export const getAssetTree = async (): Promise<AssetTreeItem[]> => {
  const res = await apiClient.get<{ code: number; message: string; data: AssetTreeItem[] }>('/api/assets/tree');
  if (res.code !== 200) {
    throw new Error(res.message || '获取资源目录树失败');
  }
  return res.data || [];
};

// 获取文件夹下的文件列表（公开接口）
export const getFolderFiles = async (folderId: number): Promise<FolderFilesResponse> => {
  const res = await apiClient.get<{ code: number; message: string; data: FolderFilesResponse }>(
    `/api/assets/folder/${folderId}`
  );
  if (res.code !== 200) {
    throw new Error(res.message || '获取文件列表失败');
  }
  return res.data;
};

// 获取下载链接（公开接口）
export const getDownloadUrl = async (id: number): Promise<{ name: string; size: number; mime_type: string; download_url: string }> => {
  const res = await apiClient.get<{ code: number; message: string; data: { name: string; size: number; mime_type: string; download_url: string } }>(
    `/api/assets/${id}/download`
  );
  if (res.code !== 200) {
    throw new Error(res.message || '获取下载链接失败');
  }
  return res.data;
};

// 格式化文件大小
export const formatFileSize = (bytes: number): string => {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
};

// 获取文件图标类型
export const getFileIcon = (mimeType?: string): string => {
  if (!mimeType) return 'file';
  if (mimeType.startsWith('image/')) return 'image';
  if (mimeType.startsWith('video/')) return 'video';
  if (mimeType.startsWith('audio/')) return 'audio';
  if (mimeType === 'application/pdf') return 'pdf';
  if (mimeType.includes('word') || mimeType.includes('document')) return 'word';
  if (mimeType.includes('excel') || mimeType.includes('spreadsheet')) return 'excel';
  if (mimeType === 'application/zip' || mimeType.includes('compressed')) return 'zip';
  return 'file';
};

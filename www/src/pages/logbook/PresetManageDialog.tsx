import { useState, useCallback, useEffect } from 'react'
import {
  Box, Typography, IconButton, Button, Dialog, DialogTitle, DialogContent,
  DialogActions, TextField, Snackbar, Alert, ListItemText, CircularProgress,
  List, ListItem,
} from '@mui/material'
import Add from '@mui/icons-material/Add'
import Edit from '@mui/icons-material/Edit'
import Delete from '@mui/icons-material/Delete'
import DragIndicator from '@mui/icons-material/DragIndicator'
import {
  DndContext, closestCenter, KeyboardSensor, PointerSensor, useSensor, useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  arrayMove, SortableContext, sortableKeyboardCoordinates, useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { apiClient } from '../../services/api'
import { buildPresetOrders } from './presetOrder'
import type { RadioPreset, RadioPresetListResponse } from './types'
interface PresetManageDialogProps {
  open: boolean
  onClose: () => void
  onRefresh: () => void
}

// 可排序的预设列表项
interface SortablePresetItemProps {
  preset: RadioPreset
  onEdit: (preset: RadioPreset) => void
  onDelete: (id: number) => void
}

function SortablePresetItem({ preset, onEdit, onDelete }: SortablePresetItemProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: preset.id })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    backgroundColor: isDragging ? 'action.hover' : 'transparent',
  }

  return (
    <ListItem
      ref={setNodeRef}
      style={style}
      secondaryAction={
        <Box>
          <IconButton size="small" onClick={() => onEdit(preset)}>
            <Edit fontSize="small" />
          </IconButton>
          <IconButton size="small" color="error" onClick={() => onDelete(preset.id)}>
            <Delete fontSize="small" />
          </IconButton>
        </Box>
      }
    >
      <Box {...attributes} {...listeners} sx={{ cursor: 'grab', mr: 1, display: 'flex', alignItems: 'center' }}>
        <DragIndicator color="action" />
      </Box>
      <ListItemText
        primary={preset.name}
        secondary={
          <Typography variant="body2" color="text.secondary">
            {preset.radio} / {preset.antenna}
            {preset.power && ` / ${preset.power}W`}
            {preset.qth && ` / ${preset.qth}`}
          </Typography>
        }
      />
    </ListItem>
  )
}

// 预设管理对话框
export function PresetManageDialog({ open, onClose, onRefresh }: PresetManageDialogProps) {
  const [presets, setPresets] = useState<RadioPreset[]>([])
  const [loading, setLoading] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [editingPreset, setEditingPreset] = useState<RadioPreset | null>(null)
  const [snackbar, setSnackbar] = useState<{ open: boolean; message: string; severity: 'success' | 'error' }>({ open: false, message: '', severity: 'success' })
  const [formData, setFormData] = useState({
    name: '',
    radio: '',
    antenna: '',
    power: '' as number | '',
    qth: ''
  })

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  )

  const loadPresets = useCallback(async () => {
    setLoading(true)
    try {
      const response = await apiClient.get<RadioPresetListResponse>('/api/user/radio-presets')
      if (response.code >= 200 && response.code < 300 && response.data) {
        setPresets(response.data)
      }
    } catch (error) {
      console.error('加载预设失败:', error)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) {
      loadPresets()
    }
  }, [open, loadPresets])

  const handleAdd = () => {
    setEditingPreset(null)
    setFormData({ name: '', radio: '', antenna: '', power: '', qth: '' })
    setEditDialogOpen(true)
  }

  const handleEdit = (preset: RadioPreset) => {
    setEditingPreset(preset)
    setFormData({
      name: preset.name,
      radio: preset.radio,
      antenna: preset.antenna,
      power: preset.power ?? '',
      qth: preset.qth || ''
    })
    setEditDialogOpen(true)
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定要删除这个预设吗？')) return

    try {
      const response = await apiClient.delete(`/api/user/radio-presets/${id}`)
      if (response.code >= 200 && response.code < 300) {
        setSnackbar({ open: true, message: '删除成功', severity: 'success' })
        loadPresets()
        onRefresh()
      } else {
        setSnackbar({ open: true, message: response.message || '删除失败', severity: 'error' })
      }
    } catch (error) {
      console.error('删除预设失败:', error)
      setSnackbar({ open: true, message: '删除失败', severity: 'error' })
    }
  }

  const handleSave = async () => {
    if (!formData.name || !formData.radio || !formData.antenna) {
      setSnackbar({ open: true, message: '请填写必填项', severity: 'error' })
      return
    }

    try {
      const data = {
        name: formData.name,
        radio: formData.radio,
        antenna: formData.antenna,
        power: formData.power || null,
        qth: formData.qth || null
      }

      let response
      if (editingPreset) {
        response = await apiClient.put(`/api/user/radio-presets/${editingPreset.id}`, data)
      } else {
        response = await apiClient.post('/api/user/radio-presets', data)
      }

      if (response.code >= 200 && response.code < 300) {
        setSnackbar({ open: true, message: editingPreset ? '保存成功' : '添加成功', severity: 'success' })
        setEditDialogOpen(false)
        loadPresets()
        onRefresh()
      } else {
        setSnackbar({ open: true, message: response.message || '操作失败', severity: 'error' })
      }
    } catch (error) {
      console.error('保存预设失败:', error)
      setSnackbar({ open: true, message: '操作失败', severity: 'error' })
    }
  }

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event

    if (over && active.id !== over.id) {
      const oldIndex = presets.findIndex(p => p.id === active.id)
      const newIndex = presets.findIndex(p => p.id === over.id)

      const newPresets = arrayMove(presets, oldIndex, newIndex)
      setPresets(newPresets)

      // 保存排序到后端
      try {
        const orders = buildPresetOrders(newPresets)
        await apiClient.put('/api/user/radio-presets/reorder', { orders })
        onRefresh()
      } catch (error) {
        console.error('保存排序失败:', error)
        setSnackbar({ open: true, message: '保存排序失败', severity: 'error' })
        loadPresets() // 恢复原顺序
      }
    }
  }

  return (
    <>
      <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
        <DialogTitle>
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <Typography variant="h6">管理电台预设</Typography>
            <Button startIcon={<Add />} onClick={handleAdd} variant="contained" size="small">
              添加预设
            </Button>
          </Box>
        </DialogTitle>
        <DialogContent>
          {loading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
              <CircularProgress />
            </Box>
          ) : presets.length === 0 ? (
            <Box sx={{ textAlign: 'center', py: 4, color: 'text.secondary' }}>
              <Typography>暂无预设，点击上方按钮添加</Typography>
            </Box>
          ) : (
            <DndContext
              sensors={sensors}
              collisionDetection={closestCenter}
              onDragEnd={handleDragEnd}
            >
              <SortableContext
                items={presets.map(p => p.id)}
                strategy={verticalListSortingStrategy}
              >
                <List>
                  {presets.map((preset) => (
                    <SortablePresetItem
                      key={preset.id}
                      preset={preset}
                      onEdit={handleEdit}
                      onDelete={handleDelete}
                    />
                  ))}
                </List>
              </SortableContext>
            </DndContext>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>关闭</Button>
        </DialogActions>
      </Dialog>

      {/* 添加/编辑预设弹窗 */}
      <Dialog open={editDialogOpen} onClose={() => setEditDialogOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>{editingPreset ? '编辑预设' : '添加预设'}</DialogTitle>
        <DialogContent>
          <Box sx={{ pt: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              fullWidth
              required
              label="预设名称"
              size="small"
              value={formData.name}
              onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
              placeholder="例如: 家里台"
            />
            <TextField
              fullWidth
              required
              label="电台设备"
              size="small"
              value={formData.radio}
              onChange={(e) => setFormData(prev => ({ ...prev, radio: e.target.value }))}
              placeholder="例如: IC-7300"
            />
            <TextField
              fullWidth
              required
              label="天线"
              size="small"
              value={formData.antenna}
              onChange={(e) => setFormData(prev => ({ ...prev, antenna: e.target.value }))}
              placeholder="例如: DP天线"
            />
            <TextField
              fullWidth
              label="功率 (W)"
              size="small"
              type="number"
              value={formData.power}
              onChange={(e) => setFormData(prev => ({ ...prev, power: e.target.value ? Number(e.target.value) : '' }))}
              placeholder="例如: 100"
            />
            <TextField
              fullWidth
              label="QTH"
              size="small"
              value={formData.qth}
              onChange={(e) => setFormData(prev => ({ ...prev, qth: e.target.value }))}
              placeholder="例如: 广东省广州市"
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditDialogOpen(false)}>取消</Button>
          <Button onClick={handleSave} variant="contained">保存</Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} onClose={() => setSnackbar(prev => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </>
  )
}

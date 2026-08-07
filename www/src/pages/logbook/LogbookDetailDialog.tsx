import {
  Paper, Typography, Button, Chip, Dialog, DialogTitle, DialogContent,
  DialogActions, Grid,
} from '@mui/material'
import { utcToBjt } from './time'
import type { LogbookEntry } from './types'
interface LogbookDetailDialogProps {
  open: boolean
  onClose: () => void
  entry: LogbookEntry | null
  timeDisplayMode: 'bjt' | 'utc'
}

export function LogbookDetailDialog({ open, onClose, entry, timeDisplayMode }: LogbookDetailDialogProps) {
  if (!entry) return null

  const timeLabel = timeDisplayMode === 'bjt' ? '北京时间 (BJT)' : '协调世界时 (UTC)'
  const timeValue = timeDisplayMode === 'bjt' ? utcToBjt(entry.time_utc) : entry.time_utc
  const isSameFrequency = entry.tx_frequency === entry.rx_frequency

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>
        通联详情
        <Chip label={entry.callsign} color="primary" size="small" sx={{ ml: 2 }} />
      </DialogTitle>
      <DialogContent dividers>
        <Grid container spacing={2}>
          {/* 基本信息 */}
          <Grid size={12}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              基本信息
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Grid container spacing={1}>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">{timeLabel}</Typography>
                  <Typography variant="body2">{timeValue}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">通信模式</Typography>
                  <Typography variant="body2"><Chip label={entry.mode} size="small" /></Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">发射频率</Typography>
                  <Typography variant="body2">{entry.tx_frequency} MHz</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">接收频率</Typography>
                  <Typography variant="body2">
                    {isSameFrequency ? '同频' : `${entry.rx_frequency} MHz`}
                  </Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">CQ 分区</Typography>
                  <Typography variant="body2">{entry.cq_zone}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">ITU 分区</Typography>
                  <Typography variant="body2">{entry.itu_zone}</Typography>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 信号报告 */}
          <Grid size={12}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              信号报告
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Grid container spacing={1}>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">收信报告 (对方)</Typography>
                  <Typography variant="body2" fontWeight="medium">{entry.their_rst}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">发信报告 (我方)</Typography>
                  <Typography variant="body2" fontWeight="medium">{entry.my_rst}</Typography>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 对方信息 */}
          <Grid size={12}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              对方信息
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Grid container spacing={1}>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">QTH</Typography>
                  <Typography variant="body2">{entry.their_qth || '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">功率</Typography>
                  <Typography variant="body2">{entry.their_power ? `${entry.their_power} W` : '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">设备</Typography>
                  <Typography variant="body2">{entry.their_radio || '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">天馈</Typography>
                  <Typography variant="body2">{entry.their_antenna || '-'}</Typography>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 我方信息 */}
          <Grid size={12}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              我方信息
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <Grid container spacing={1}>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">QTH</Typography>
                  <Typography variant="body2">{entry.my_qth || '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">功率</Typography>
                  <Typography variant="body2">{entry.my_power ? `${entry.my_power} W` : '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">设备</Typography>
                  <Typography variant="body2">{entry.my_radio || '-'}</Typography>
                </Grid>
                <Grid size={6}>
                  <Typography variant="caption" color="text.secondary">天馈</Typography>
                  <Typography variant="body2">{entry.my_antenna || '-'}</Typography>
                </Grid>
              </Grid>
            </Paper>
          </Grid>

          {/* 备注 */}
          {entry.notes && (
            <Grid size={12}>
              <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                备注
              </Typography>
              <Paper variant="outlined" sx={{ p: 2 }}>
                <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>
                  {entry.notes}
                </Typography>
              </Paper>
            </Grid>
          )}
        </Grid>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>关闭</Button>
      </DialogActions>
    </Dialog>
  )
}

// 预设管理对话框属性

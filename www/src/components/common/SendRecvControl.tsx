import { Stack, Button, Tooltip } from '@mui/material'

interface SendRecvControlProps {
  disableSend: boolean
  disableRecv: boolean
  onToggleSend: () => void
  onToggleRecv: () => void
  disabled?: boolean
  size?: 'small' | 'medium'
}

export function SendRecvControl({
  disableSend,
  disableRecv,
  onToggleSend,
  onToggleRecv,
  disabled = false,
  size = 'small',
}: SendRecvControlProps) {
  return (
    <Stack direction="row" spacing={1} alignItems="center">
      {/* 发送控制 */}
      <Tooltip title={disableSend ? '点击启用发送' : '点击禁用发送'}>
        <span>
          <Button
            size={size}
            variant={disableSend ? 'outlined' : 'contained'}
            color={disableSend ? 'error' : 'success'}
            onClick={onToggleSend}
            disabled={disabled}
            sx={{ minWidth: 56, fontSize: '0.75rem' }}
          >
            发送
          </Button>
        </span>
      </Tooltip>

      {/* 接收控制 */}
      <Tooltip title={disableRecv ? '点击启用接收' : '点击禁用接收'}>
        <span>
          <Button
            size={size}
            variant={disableRecv ? 'outlined' : 'contained'}
            color={disableRecv ? 'error' : 'success'}
            onClick={onToggleRecv}
            disabled={disabled}
            sx={{ minWidth: 56, fontSize: '0.75rem' }}
          >
            接收
          </Button>
        </span>
      </Tooltip>
    </Stack>
  )
}

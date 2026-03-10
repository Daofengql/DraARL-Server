import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
} from '@mui/material'
import {
  ErrorOutline as ErrorIcon,
  Warning as WarningIcon,
  Info as InfoIcon,
} from '@mui/icons-material'

interface ConfirmDialogProps {
  isOpen: boolean
  title: string
  message: string
  confirmText?: string
  cancelText?: string
  onConfirm: () => void
  onCancel: () => void
  type?: 'danger' | 'warning' | 'info'
}

const typeConfig = {
  danger: {
    icon: <ErrorIcon fontSize="large" color="error" />,
    color: 'error' as const,
  },
  warning: {
    icon: <WarningIcon fontSize="large" color="warning" />,
    color: 'warning' as const,
  },
  info: {
    icon: <InfoIcon fontSize="large" color="info" />,
    color: 'primary' as const,
  },
}

export function ConfirmDialog({
  isOpen,
  title,
  message,
  confirmText = '确认',
  cancelText = '取消',
  onConfirm,
  onCancel,
  type = 'info',
}: ConfirmDialogProps) {
  return (
    <Dialog open={isOpen} onClose={onCancel} maxWidth="sm" fullWidth>
      <Box sx={{ textAlign: 'center', pt: 3, pb: 1 }}>
        {typeConfig[type].icon}
      </Box>
      <DialogTitle sx={{ textAlign: 'center', fontSize: '1.25rem', fontWeight: 600 }}>
        {title}
      </DialogTitle>
      <DialogContent sx={{ textAlign: 'center', pb: 2 }}>
        {message}
      </DialogContent>
      <DialogActions sx={{ justifyContent: 'center', pb: 3, gap: 1 }}>
        <Button onClick={onCancel} variant="outlined" size="small">
          {cancelText}
        </Button>
        <Button
          onClick={onConfirm}
          variant="contained"
          color={typeConfig[type].color}
          size="small"
        >
          {confirmText}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

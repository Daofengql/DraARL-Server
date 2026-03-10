import type { ReactNode } from 'react'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  IconButton,
  Box,
  type Breakpoint,
} from '@mui/material'
import { Close } from '@mui/icons-material'

interface ModalProps {
  isOpen: boolean
  onClose: () => void
  title: string | ReactNode
  children: ReactNode
  footer?: ReactNode
  size?: Breakpoint
}

export function Modal({
  isOpen,
  onClose,
  title,
  children,
  footer,
  size = 'md',
}: ModalProps) {
  return (
    <Dialog
      open={isOpen}
      onClose={onClose}
      maxWidth={size}
      fullWidth
      PaperProps={{
        sx: {
          maxHeight: '85vh',
          display: 'flex',
          flexDirection: 'column',
        },
      }}
    >
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          px: 2,
          py: 1,
          borderBottom: 1,
          borderColor: 'divider',
        }}
      >
        <DialogTitle sx={{ p: 0, fontSize: '1.25rem', fontWeight: 600 }}>
          {title}
        </DialogTitle>
        <IconButton onClick={onClose} size="small">
          <Close />
        </IconButton>
      </Box>

      <DialogContent
        sx={{
          flex: 1,
          overflowY: 'auto',
          p: 2,
        }}
      >
        {children}
      </DialogContent>

      {footer && (
        <DialogActions
          sx={{
            px: 2,
            py: 1,
            borderTop: 1,
            borderColor: 'divider',
            bgcolor: 'background.default',
          }}
        >
          {footer}
        </DialogActions>
      )}
    </Dialog>
  )
}

import { useEffect, useState } from 'react'
import {
  Alert,
  Box,
  IconButton,
  Collapse,
} from '@mui/material'
import Close from '@mui/icons-material/Close'
import CheckCircle from '@mui/icons-material/CheckCircle'
import Error from '@mui/icons-material/Error'
import Warning from '@mui/icons-material/Warning'
import Info from '@mui/icons-material/Info'

interface Toast {
  id: number
  message: string
  type: 'success' | 'error' | 'warning' | 'info'
}

let toastId = 0
const listeners: ((toasts: Toast[]) => void)[] = []
let toasts: Toast[] = []

const notifyListeners = () => {
  listeners.forEach((listener) => listener([...toasts]))
}

export const toast = {
  add: (message: string, type: Toast['type']) => {
    const id = toastId++
    toasts.push({ id, message, type })
    notifyListeners()

    setTimeout(() => {
      toast.remove(id)
    }, 3000)
  },
  success: (message: string) => toast.add(message, 'success'),
  error: (message: string) => toast.add(message, 'error'),
  warning: (message: string) => toast.add(message, 'warning'),
  info: (message: string) => toast.add(message, 'info'),

  remove: (id: number) => {
    toasts = toasts.filter((t) => t.id !== id)
    notifyListeners()
  },
}

const typeConfig = {
  success: {
    icon: <CheckCircle fontSize="inherit" />,
    severity: 'success' as const,
  },
  error: {
    icon: <Error fontSize="inherit" />,
    severity: 'error' as const,
  },
  warning: {
    icon: <Warning fontSize="inherit" />,
    severity: 'warning' as const,
  },
  info: {
    icon: <Info fontSize="inherit" />,
    severity: 'info' as const,
  },
}

export function ToastContainer() {
  const [toastList, setToastList] = useState<Toast[]>([])

  useEffect(() => {
    const listener = (newToasts: Toast[]) => setToastList(newToasts)
    listeners.push(listener)
    return () => {
      const index = listeners.indexOf(listener)
      if (index > -1) listeners.splice(index, 1)
    }
  }, [])

  return (
    <Box
      sx={{
        position: 'fixed',
        top: { xs: 12, sm: 16 },
        right: { xs: 12, sm: 16 },
        zIndex: 9999,
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
        maxWidth: { xs: 'calc(100vw - 24px)', sm: 400 },
      }}
    >
      {toastList.map((t) => (
        <Collapse key={t.id} in>
          <Alert
            severity={typeConfig[t.type].severity}
            icon={typeConfig[t.type].icon}
            action={
              <IconButton
                size="small"
                color="inherit"
                onClick={() => toast.remove(t.id)}
              >
                <Close fontSize="small" />
              </IconButton>
            }
            sx={{ boxShadow: 2 }}
          >
            {t.message}
          </Alert>
        </Collapse>
      ))}
    </Box>
  )
}

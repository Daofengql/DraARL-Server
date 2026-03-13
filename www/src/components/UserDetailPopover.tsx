import {
  Card,
  CardContent,
  List,
  ListItem,
  ListItemText,
  Divider,
  Avatar,
  Box,
  Typography,
  Chip,
  Popover,
} from '@mui/material'
import type { PopoverProps } from '@mui/material'
import {
  Badge,
  Phone,
  Cake,
  LocationOn,
  Person,
  CalendarToday,
  CheckCircle,
} from '@mui/icons-material'
import type { User } from '../types'

interface UserDetailPopoverProps {
  open: boolean
  anchorEl: PopoverProps['anchorEl']
  onClose: () => void
  user: User | null
}

export function UserDetailPopover({ open, anchorEl, onClose, user }: UserDetailPopoverProps) {
  const formatDate = (dateStr?: string) => {
    if (!dateStr) return '-'
    return new Date(dateStr).toLocaleDateString('zh-CN')
  }

  const getSexLabel = (sex: number) => {
    switch (sex) {
      case 1:
        return '男'
      case 2:
        return '女'
      default:
        return '未设置'
    }
  }

  if (!user) return null

  return (
    <Popover
      open={open}
      anchorEl={anchorEl}
      onClose={onClose}
      anchorOrigin={{
        vertical: 'bottom',
        horizontal: 'left',
      }}
      transformOrigin={{
        vertical: 'top',
        horizontal: 'left',
      }}
      slotProps={{
        paper: {
          sx: { width: 400, maxHeight: 600, overflow: 'auto' },
        },
      }}
    >
      <Card>
        <CardContent>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2 }}>
            <Avatar
              src={user.avatar_thumb || user.avatar}
              sx={{ width: 64, height: 64 }}
            >
              {user.username?.charAt(0).toUpperCase()}
            </Avatar>
            <Box>
              <Typography variant="h6">{user.username}</Typography>
              <Typography variant="body2" color="text.secondary">
                ID: {user.id}
              </Typography>
              <Box sx={{ display: 'flex', gap: 0.5, mt: 0.5 }}>
                <Chip
                  label={user.role === 'admin' ? '管理员' : '普通用户'}
                  size="small"
                  color={user.role === 'admin' ? 'secondary' : 'default'}
                />
                <Chip
                  label={user.status === 1 ? '正常' : '已禁用'}
                  size="small"
                  color={user.status === 1 ? 'success' : 'error'}
                />
              </Box>
            </Box>
          </Box>

          <Divider sx={{ mb: 2 }} />

          <List disablePadding dense>
            {user.callsign && (
              <ListItem divider>
                <Badge sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                <ListItemText
                  primary="呼号"
                  secondary={user.callsign}
                />
              </ListItem>
            )}
            {user.phone && (
              <ListItem divider>
                <Phone sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                <ListItemText
                  primary="手机号"
                  secondary={user.phone}
                />
              </ListItem>
            )}
            {user.address && (
              <ListItem divider>
                <LocationOn sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                <ListItemText
                  primary="地址"
                  secondary={user.address}
                />
              </ListItem>
            )}
            {user.birthday && (
              <ListItem divider>
                <Cake sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                <ListItemText
                  primary="生日"
                  secondary={user.birthday}
                />
              </ListItem>
            )}
            {user.sex !== undefined && user.sex !== 0 && (
              <ListItem divider>
                <Person sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                <ListItemText
                  primary="性别"
                  secondary={getSexLabel(user.sex)}
                />
              </ListItem>
            )}
            <ListItem divider>
              <CalendarToday sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
              <ListItemText
                primary="注册时间"
                secondary={formatDate(user.created_at)}
              />
            </ListItem>
            {user.last_login_time && (
              <ListItem divider>
                <CheckCircle sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                <ListItemText
                  primary="最后登录"
                  secondary={new Date(user.last_login_time).toLocaleString('zh-CN')}
                />
              </ListItem>
            )}
            {user.dmrid && user.dmrid > 0 && (
              <ListItem divider>
                <Badge sx={{ mr: 2, color: 'text.secondary' }} fontSize="small" />
                <ListItemText
                  primary="DMR ID"
                  secondary={user.dmrid}
                />
              </ListItem>
            )}
            {user.introduction && (
              <ListItem>
                <Typography variant="body2" color="text.secondary">
                  简介: {user.introduction}
                </Typography>
              </ListItem>
            )}
          </List>
        </CardContent>
      </Card>
    </Popover>
  )
}

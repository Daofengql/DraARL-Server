import { Box, Typography, Button, Container } from '@mui/material'
import { Construction, Login } from '@mui/icons-material'
import { useNavigate } from 'react-router-dom'

export function HomePage() {
  const navigate = useNavigate()

  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
      }}
    >
      <Container maxWidth="sm">
        <Box
          sx={{
            textAlign: 'center',
            p: 4,
            borderRadius: 4,
            bgcolor: 'rgba(255, 255, 255, 0.95)',
            boxShadow: '0 20px 60px rgba(0,0,0,0.3)',
          }}
        >
          <Construction sx={{ fontSize: 80, color: 'warning.main', mb: 2 }} />
          <Typography variant="h4" gutterBottom sx={{ fontWeight: 'bold', color: 'text.primary' }}>
            网站维护中
          </Typography>
          <Typography variant="body1" color="text.secondary" sx={{ mb: 4 }}>
            系统正在进行升级维护，请稍后再访问。
          </Typography>
          <Button
            variant="contained"
            size="large"
            startIcon={<Login />}
            onClick={() => navigate('/login')}
            sx={{
              px: 4,
              py: 1.5,
              borderRadius: 2,
              textTransform: 'none',
              fontSize: '1rem',
            }}
          >
            前往登录
          </Button>
        </Box>
      </Container>
    </Box>
  )
}

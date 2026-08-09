import { useState } from 'react'
import {
  Box,
  Button,
  Card,
  CardContent,
  Grid,
  Stack,
  Typography,
} from '@mui/material'
import Build from '@mui/icons-material/Build'
import Tune from '@mui/icons-material/Tune'
import BluetoothSearching from '@mui/icons-material/BluetoothSearching'
import { PublicPageLayout } from '../../components/layout'
import { PreConfigToolCard } from '../../components/devices/preconfig/PreConfigToolCard'
import { usePageTitle } from '../../hooks/usePageTitle'

export function ToolsPage() {
  usePageTitle()

  const [preConfigToolOpen, setPreConfigToolOpen] = useState(false)

  return (
    <PublicPageLayout maxWidth="lg" centered={false}>
      <Stack spacing={3}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Build color="primary" sx={{ fontSize: 32 }} />
          <Typography variant="h4">工具</Typography>
        </Box>

        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <Card variant="outlined" sx={{ height: '100%', borderRadius: 2 }}>
              <CardContent sx={{ p: 3 }}>
                <Stack spacing={2}>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                    <Tune color="primary" />
                    <Box sx={{ minWidth: 0 }}>
                      <Typography variant="h6">设备配置工具</Typography>
                      <Typography variant="body2" color="text.secondary">
                        通过 BLE 写入设备的 WiFi 与 DraARL 连接配置。
                      </Typography>
                    </Box>
                  </Box>

                  <Button
                    variant="contained"
                    startIcon={<BluetoothSearching />}
                    onClick={() => setPreConfigToolOpen(true)}
                    sx={{ alignSelf: 'flex-start' }}
                  >
                    打开预配置工具
                  </Button>
                </Stack>
              </CardContent>
            </Card>
          </Grid>
        </Grid>
      </Stack>

      <PreConfigToolCard
        open={preConfigToolOpen}
        onClose={() => setPreConfigToolOpen(false)}
      />
    </PublicPageLayout>
  )
}

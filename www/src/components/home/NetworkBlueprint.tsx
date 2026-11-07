import { useId } from 'react'
import { Box, Typography, useTheme } from '@mui/material'

export function NetworkBlueprint() {
  const titleId = useId()
  const theme = useTheme()
  const line = theme.palette.secondary.main
  const muted = theme.palette.text.secondary
  const ink = theme.palette.text.primary
  const paper = theme.palette.background.paper

  return (
    <Box component="figure" sx={{ m: 0, minWidth: 0, position: 'relative' }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', gap: 2, borderBottom: '1px solid', borderColor: 'divider', pb: 1, mb: 1 }}>
        <Typography variant="caption" color="text.secondary">通信结构 / SCHEMATIC</Typography>
        <Typography variant="caption" color="secondary.main">DraARLv1</Typography>
      </Box>
      <Box component="svg" role="img" aria-labelledby={titleId} viewBox="0 0 600 460" sx={{ display: 'block', width: '100%', height: 'auto', overflow: 'visible' }}>
        <title id={titleId}>设备和浏览器接入 DraARL 中心，通过群组路由和边缘节点进行通信的结构示意。</title>
        <g fill="none" stroke={line} strokeWidth="1.2">
          <path d="M10 28V10h18 M572 10h18v18 M590 432v18h-18 M28 450H10v-18" />
          <path d="M192 38h218 M192 33v10 M410 33v10 M564 142v180 M559 142h10 M559 322h10" stroke={muted} opacity=".55" />
          <path d="M120 154h60v50h48 M120 344h60v-92h48 M410 206h66v-84 M410 252h66v96" strokeDasharray="5 5" />
          <circle cx="180" cy="204" r="3" fill={paper} />
          <circle cx="180" cy="252" r="3" fill={paper} />
          <circle cx="476" cy="206" r="3" fill={paper} />
          <circle cx="476" cy="252" r="3" fill={paper} />
          <path d="M220 122h188l16 16v186l-16 16H220l-16-16V138z" fill={paper} />
          <rect x="220" y="138" width="188" height="186" />
          <path d="M228 178h172 M228 278h172 M228 288h172 M228 298h172 M228 308h172" opacity=".45" />
          <path d="M240 112V92h152v20 M256 92V80 M376 92V80" />
          <path d="M244 358h140 M252 352v12 M376 352v12" stroke={muted} opacity=".6" />
          <circle cx="232" cy="150" r="2" /><circle cx="396" cy="150" r="2" />
          <circle cx="232" cy="312" r="2" /><circle cx="396" cy="312" r="2" />
          <rect x="42" y="114" width="78" height="94" fill={paper} />
          <path d="M56 114V76 M53 76h6 M56 76l-6-12 M68 126h40v30H68z M58 176h46 M58 184h46 M58 192h46" />
          <circle cx="65" cy="164" r="3" /><circle cx="83" cy="164" r="3" />
          <rect x="28" y="292" width="112" height="76" fill={paper} />
          <path d="M28 308h112 M72 368v12 M96 368v12 M56 380h56 M35 300h2 M43 300h2 M51 300h2" />
          <path d="m68 327-9 9 9 9 M96 327l9 9-9 9 M87 323l-10 26" />
          <rect x="438" y="58" width="112" height="62" fill={paper} />
          <path d="M458 80h72 M458 100h72 M480 80v20 M508 80v20" />
          <rect x="438" y="348" width="112" height="62" fill={paper} />
          <path d="M450 361h88 M450 376h88 M450 391h88" opacity=".6" />
          <path d="M454 360h4 M454 375h4 M454 390h4" strokeWidth="2" />
          <path d="M32 228h100 M34 224v8 M130 224v8 M154 96h22l24 32 M424 319l21 12h95" stroke={muted} opacity=".5" />
        </g>
        <g fill={muted} fontSize="13" letterSpacing="1">
          <text x="302" y="29" textAnchor="middle">VOICE · DATA · CONTROL</text>
          <text x="36" y="52">01 / 设备接入</text>
          <text x="28" y="277">02 / 浏览器</text>
          <text x="438" y="42">03 / 群组</text>
          <text x="438" y="438">04 / 边缘节点</text>
          <text x="157" y="183">UDP</text>
          <text x="34" y="402">WebSocket</text>
          <text x="304" y="383" textAnchor="middle">认证 · 会话 · 路由</text>
        </g>
        <g fill={ink} fontSize="22" fontWeight="600">
          <text x="314" y="169" textAnchor="middle">DraARL</text>
          <text x="314" y="218" textAnchor="middle" fontSize="18">通信中心</text>
        </g>
        <g fill={line} fontSize="13">
          <text x="314" y="251" textAnchor="middle">CENTER NODE</text>
        </g>
      </Box>
      <Typography component="figcaption" variant="caption" color="text.secondary" sx={{ borderTop: '1px solid', borderColor: 'divider', pt: 1, display: 'block' }}>
        设备、浏览器与边缘节点的连接示意，非实时状态。
      </Typography>
    </Box>
  )
}

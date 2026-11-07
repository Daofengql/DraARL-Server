import SvgIcon, { type SvgIconProps } from '@mui/material/SvgIcon'
import type { ReactNode } from 'react'

// A shared 24-unit drawing grid keeps navigation and data symbols consistent.
// Icons inherit the surrounding text color and retain MUI sizing/accessibility.
function createBlueprintIcon(name: string, drawing: ReactNode) {
  function BlueprintSymbol(props: SvgIconProps) {
    return (
      <SvgIcon {...props} viewBox="0 0 24 24">
        <g fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="square" strokeLinejoin="miter">
          {drawing}
        </g>
      </SvgIcon>
    )
  }
  BlueprintSymbol.displayName = name
  return BlueprintSymbol
}

export const AntennaIcon = createBlueprintIcon('AntennaIcon', <>
  <path d="M12 7v14M8 21h8M8 16l4-7 4 7M7 10a7 7 0 0 1 10 0M4 7a11 11 0 0 1 16 0" />
  <circle cx="12" cy="5" r="1.5" />
</>)

export const HomeIcon = createBlueprintIcon('HomeIcon', <>
  <path d="m2 11 10-8 10 8M5 9v12h14V9M10 21v-7h4v7" />
</>)

export const DashboardIcon = createBlueprintIcon('DashboardIcon', <>
  <path d="M3 3h10v7H3zM17 3h4v7h-4zM3 14h4v7H3zM11 14h10v7H11z" />
</>)

export const DeviceIcon = createBlueprintIcon('DeviceIcon', <>
  <path d="M6 7h12v14H6zM8 7V2M13 4h3M9 10h6v4H9zM9 18h6" />
  <path d="M20 9h1v5h-1" />
</>)

export const GroupIcon = createBlueprintIcon('GroupIcon', <>
  <path d="M9 2h6v5H9zM3 16h6v5H3zM15 16h6v5h-6zM12 7v5M6 16v-4h12v4" />
</>)

export const ProfileIcon = createBlueprintIcon('ProfileIcon', <>
  <circle cx="12" cy="7" r="3" />
  <path d="M5 21v-5l4-4h6l4 4v5M9 21h6" />
</>)

export const UsersIcon = createBlueprintIcon('UsersIcon', <>
  <circle cx="8" cy="7" r="3" /><circle cx="18" cy="8" r="2" />
  <path d="M2 21v-5l3-4h6l3 4v5M16 13h3l3 4v4" />
</>)

export const RadioIcon = createBlueprintIcon('RadioIcon', <>
  <path d="M3 10h18v11H3zM7 10 17 2M12 14h6M12 17h6M12 19h6" />
  <circle cx="7" cy="15" r="2" />
</>)

export const MicIcon = createBlueprintIcon('MicIcon', <>
  <path d="M8 4v8a4 4 0 0 0 8 0V4a4 4 0 0 0-8 0Z" />
  <path d="M4 11a8 8 0 0 0 16 0M12 19v3M8 22h8" />
</>)

export const MicOffIcon = createBlueprintIcon('MicOffIcon', <>
  <path d="M8 4v7M16 4v8a4 4 0 0 1-6.4 3.2M4 11a8 8 0 0 0 11.4 7.2M12 19v3M8 22h8M4 4l16 16" />
</>)

export const RecordIcon = createBlueprintIcon('RecordIcon', <>
  <path d="M4 7h16v14H4zM8 3v4M16 3v4M7 14h2l2-4 2 8 2-4h2" />
</>)

export const LogIcon = createBlueprintIcon('LogIcon', <>
  <path d="M6 3h14v18H6zM3 7h5M3 12h5M3 17h5M11 8h5M11 12h5M11 16h3" />
</>)

export const DocsIcon = createBlueprintIcon('DocsIcon', <>
  <path d="M4 3h10l6 6v12H4zM14 3v6h6M8 13h8M8 17h5" />
</>)

export const ForumIcon = createBlueprintIcon('ForumIcon', <>
  <path d="M3 4h18v13h-9l-5 4v-4H3zM7 8h10M7 12h6" />
</>)

export const ToolsIcon = createBlueprintIcon('ToolsIcon', <>
  <path d="m14 3-4 4v5l-7 7 2 2 7-7h5l4-4V5l-4 4h-3V6l3-3z" />
</>)

export const InfoIcon = createBlueprintIcon('InfoIcon', <>
  <path d="M4 4h16v16H4zM12 11v6M11 7h2" />
</>)

export const SettingsIcon = createBlueprintIcon('SettingsIcon', <>
  <path d="M3 4h18v16H3zM8 7v3M8 14v3M16 7v6M16 17v-4" />
  <path d="M6 10h4v4H6zM14 9h4v4h-4z" />
</>)

export const AdminIcon = createBlueprintIcon('AdminIcon', <>
  <path d="m12 2 9 4v7l-4 6-5 3-5-3-4-6V6zM9 9h6v6H9zM12 6v3M12 15v3" />
</>)

export const LoginIcon = createBlueprintIcon('LoginIcon', <>
  <path d="M11 3h10v18H11M3 12h12M11 8l4 4-4 4" />
</>)

export const LogoutIcon = createBlueprintIcon('LogoutIcon', <>
  <path d="M13 3H3v18h10M10 12h12M18 8l4 4-4 4" />
</>)

export const RegisterIcon = createBlueprintIcon('RegisterIcon', <>
  <circle cx="8" cy="7" r="3" />
  <path d="M2 21v-5l3-4h6l3 4v5M19 5v8M15 9h8" />
</>)

export const LinkIcon = createBlueprintIcon('LinkIcon', <>
  <path d="M2 7h7v10H2zM15 7h7v10h-7zM9 12h6M5 10v4M19 10v4" />
</>)

export const HubIcon = createBlueprintIcon('HubIcon', <>
  <path d="M9 9h6v6H9zM2 2h4v4H2zM18 2h4v4h-4zM2 18h4v4H2zM18 18h4v4h-4zM6 6l3 3M15 9l3-3M6 18l3-3M15 15l3 3" />
</>)

export const DownloadIcon = createBlueprintIcon('DownloadIcon', <>
  <path d="M12 2v13M7 10l5 5 5-5M4 16v5h16v-5" />
</>)

export const FolderIcon = createBlueprintIcon('FolderIcon', <>
  <path d="M3 7V4h6l3 3h9v14H3zM7 12h10M7 16h6" />
</>)

export const CheckIcon = createBlueprintIcon('CheckIcon', <>
  <path d="M3 3h18v18H3zM7 12l4 4 6-8" />
</>)

export const TimerIcon = createBlueprintIcon('TimerIcon', <>
  <circle cx="12" cy="14" r="8" />
  <path d="M9 2h6M12 2v4M12 9v5l3 2" />
</>)

export const StorageIcon = createBlueprintIcon('StorageIcon', <>
  <path d="M3 3h18v7H3zM3 14h18v7H3zM6 6h1M6 17h1M12 6h5M12 17h5" />
</>)

export const MemoryIcon = createBlueprintIcon('MemoryIcon', <>
  <path d="M2 5h20v13H2zM5 8h5v7H5zM14 8h5v7h-5zM5 18v3M9 18v3M15 18v3M19 18v3" />
</>)

export const GaugeIcon = createBlueprintIcon('GaugeIcon', <>
  <path d="M3 18a10 10 0 1 1 18 0M12 14l5-7M4 13h2M18 13h2M12 3v2M9 21h6" />
  <circle cx="12" cy="14" r="2" />
</>)

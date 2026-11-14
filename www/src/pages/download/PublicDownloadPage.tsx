import { PublicPageLayout } from '../../components/layout'
import { DownloadCenterPage } from './DownloadCenterPage'
import { usePageTitle } from '../../hooks/usePageTitle'

export function PublicDownloadPage() {
  usePageTitle()

  return (
    <PublicPageLayout maxWidth="lg" centered={false}>
      <DownloadCenterPage />
    </PublicPageLayout>
  )
}

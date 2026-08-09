import { DocsContent } from './DocsPage'
import { PublicPageLayout } from '../../components/layout'

export function PublicDocsPage() {
  return (
    <PublicPageLayout maxWidth="lg" centered={false}>
      <DocsContent />
    </PublicPageLayout>
  )
}

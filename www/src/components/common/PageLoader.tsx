import { Suspense, type ComponentType } from 'react'
import { PageContainer } from './PageContainer'

/**
 * 页面加载中的加载器组件
 */
export function PageLoader() {
  return (
    <PageContainer>
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="flex flex-col items-center gap-4">
          <div className="animate-spin rounded-full h-12 w-12 border-4 border-primary border-t-transparent" />
          <p className="text-muted-foreground text-sm">Loading...</p>
        </div>
      </div>
    </PageContainer>
  )
}

/**
 * 懒加载页面包装器的高阶组件
 * 用于包装懒加载的页面组件，提供统一的加载状态处理
 */
export function withSuspense<P extends object>(LazyComponent: ComponentType<P>) {
  return function SuspenseWrapper(props: P) {
    return (
      <Suspense fallback={<PageLoader />}>
        <LazyComponent {...props} />
      </Suspense>
    )
  }
}

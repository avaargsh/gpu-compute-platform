import { Link } from 'react-router-dom'
import { Button } from '@/components/ui/button'

export function NotFoundPage() {
  return (
    <div className="flex flex-col items-center justify-center h-screen text-center">
      <h1 className="text-h2">404</h1>
      <p className="text-muted text-sm mt-4">页面未找到</p>
      <Button asChild>
        <Link to="/dashboard">返回仪表盘</Link>
      </Button>
    </div>
  )
}

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

export function GPUMonitorPage() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-h2">GPU 监控</h1>
        <Badge variant="success">已连接</Badge>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">GPU 利用率</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-64 w-full flex items-center justify-center">
            <span className="text-muted text-sm">图表区域</span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">平均利用率</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-3xl font-bold">78%</div>
        </CardContent>
      </Card>
    </div>
  )
}

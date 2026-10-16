import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'

export function CostOptimizationPage() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-h2">成本优化</h1>
        <Button>获取推荐</Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">实例推荐</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-muted text-sm">暂无推荐数据</div>
        </CardContent>
      </Card>

      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg font-semibold">当前成本</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">¥0.00</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-lg font-semibold">优化后成本</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">¥0.00</div>
            <Badge variant="secondary">节省 0%</Badge>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import {
  LayoutDashboard,
  List,
  Monitor,
  PieChart,
} from '@/components/ui/icons'

const statCards = [
  { title: '运行中任务', value: '12', Icon: List, color: 'text-primary', border: 'border-primary/20' },
  { title: 'GPU 利用率', value: '78%', Icon: Monitor, color: 'text-success', border: 'border-success/20' },
  { title: '今日成本', value: '¥1,245', Icon: PieChart, color: 'text-accent', border: 'border-accent/20' },
  { title: '系统状态', value: '正常', Icon: LayoutDashboard, color: 'text-success', border: 'border-success/20' },
]

export function DashboardPage() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-h2">GPU 计算平台</h1>
          <p className="text-muted text-sm mt-1">集群实时概览，数据每 5 秒刷新一次</p>
        </div>
        <Button variant="outline" size="sm">刷新</Button>
      </div>

      {/* 统计指标：无圆角卡片壳，用左侧色块区分 */}
      <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
        {statCards.map((card) => {
          const Icon = card.Icon
          return (
            <div
              key={card.title}
              className={`
                rounded-lg p-4 bg-card border border-border flex flex-col gap-2
                ${card.border}
              `}
            >
              <div className="flex items-end gap-1.5">
                <Icon className="w-5 h-5 text-muted" />
                <div className="text-xl font-bold">{card.value}</div>
                <span className="text-[10px] font-medium {card.color}">
                  {card.title}
                </span>
              </div>
            </div>
          )
        })}
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-semibold">近期任务</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[40%]">任务名称</TableHead>
                  <TableHead className="w-[20%]">状态</TableHead>
                  <TableHead className="w-[20%]">进度</TableHead>
                  <TableHead className="text-right w-[20%]">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {[
                  { name: '单细胞扰动预测', status: 'running', progress: 65 },
                  { name: 'RNA-seq 表达分析', status: 'completed', progress: 100 },
                  { name: '蛋白-配体预测', status: 'failed', progress: 0 },
                ].map((t) => (
                  <TableRow key={t.name}>
                    <TableCell>{t.name}</TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          t.status === 'running'
                            ? 'default'
                            : t.status === 'completed'
                              ? 'success'
                              : 'destructive'
                        }
                      >
                        {t.status}
                      </Badge>
                    </TableCell>
                    <TableCell>{t.progress}%</TableCell>
                    <TableCell className="text-right">
                      <Button variant="outline" size="sm">详情</Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-semibold">GPU 资源分布</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-48 w-full flex items-center justify-center">
              <span className="text-muted text-sm">图表区域</span>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

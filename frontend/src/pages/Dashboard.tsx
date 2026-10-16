import { useQuery } from '@tanstack/react-query'
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
import { useTasksQuery } from '@/hooks/useTasks'
import { useGpuMetricsQuery } from '@/hooks/useGpu'
import { systemApi } from '@/services/systemApi'
import { queryKeys } from '@/lib/queryKeys'
import { Link } from 'react-router-dom'

export function DashboardPage() {
  const { data: tasks, isLoading: tasksLoading } = useTasksQuery()
  const { data: gpuMetrics, isLoading: gpuLoading } = useGpuMetricsQuery()

  const { data: systemStatus } = useQuery({
    queryKey: queryKeys.system.status,
    queryFn: () => systemApi.getStatus(),
    refetchInterval: 15000,
    retry: false,
  })

  const runningTasks = tasks?.filter((t) => t.status === 'running').length ?? null
  const avgGpuUsage = gpuMetrics?.gpus?.length
    ? Math.round(gpuMetrics.total_usage / gpuMetrics.gpus.length)
    : null
  const systemHealthy = systemStatus ? (systemStatus.status === 'ok' || systemStatus.healthy === true) : null

  const statCards = [
    {
      title: '运行中任务',
      value: tasksLoading ? '…' : runningTasks !== null ? String(runningTasks) : '不可用',
      Icon: List,
      border: 'border-primary/20',
    },
    {
      title: 'GPU 利用率',
      value: gpuLoading ? '…' : avgGpuUsage !== null ? `${avgGpuUsage}%` : '不可用',
      Icon: Monitor,
      border: 'border-success/20',
    },
    {
      title: '今日成本',
      value: '不可用',
      Icon: PieChart,
      border: 'border-accent/20',
    },
    {
      title: '系统状态',
      value: systemHealthy === null ? '不可用' : systemHealthy ? '正常' : '异常',
      Icon: LayoutDashboard,
      border: systemHealthy === false ? 'border-destructive/20' : 'border-success/20',
    },
  ]

  const recentTasks = (tasks ?? []).slice(0, 5)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-h2">GPU 计算平台</h1>
          <p className="text-muted text-sm mt-1">集群实时概览，GPU 数据每 5 秒刷新</p>
        </div>
        <Button variant="outline" size="sm">刷新</Button>
      </div>

      <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
        {statCards.map((card) => {
          const Icon = card.Icon
          return (
            <div
              key={card.title}
              className={`rounded-lg p-4 bg-card border border-border flex flex-col gap-2 ${card.border}`}
            >
              <div className="flex items-end gap-1.5">
                <Icon className="w-5 h-5 text-muted" />
                <div className="text-xl font-bold">{card.value}</div>
                <span className="text-[10px] font-medium text-muted">
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
            {tasksLoading ? (
              <div className="text-muted text-sm py-8 text-center">加载中…</div>
            ) : recentTasks.length === 0 ? (
              <div className="text-muted text-sm py-8 text-center">暂无任务数据</div>
            ) : (
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
                  {recentTasks.map((t) => (
                    <TableRow key={t.id}>
                      <TableCell>{t.name}</TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            t.status === 'running'
                              ? 'default'
                              : t.status === 'completed'
                                ? 'success'
                                : t.status === 'failed'
                                  ? 'destructive'
                                  : 'muted'
                          }
                        >
                          {t.status}
                        </Badge>
                      </TableCell>
                      <TableCell>{t.progress}%</TableCell>
                      <TableCell className="text-right">
                        <Button variant="outline" size="sm" asChild>
                          <Link to={`/tasks/${t.id}`}>详情</Link>
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-semibold">GPU 资源分布</CardTitle>
          </CardHeader>
          <CardContent>
            {gpuLoading ? (
              <div className="h-48 w-full flex items-center justify-center">
                <span className="text-muted text-sm">加载中…</span>
              </div>
            ) : gpuMetrics?.gpus?.length ? (
              <div className="space-y-2">
                {gpuMetrics.gpus.map((gpu) => (
                  <div key={gpu.id} className="flex items-center gap-3 text-sm">
                    <span className="w-24 truncate">{gpu.name}</span>
                    <div className="flex-1 bg-muted rounded-full h-2">
                      <div
                        className="bg-primary rounded-full h-2 transition-all"
                        style={{ width: `${Math.min(100, gpu.usage)}%` }}
                      />
                    </div>
                    <span className="w-12 text-right">{gpu.usage}%</span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="h-48 w-full flex items-center justify-center">
                <span className="text-muted text-sm">暂无 GPU 数据</span>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Plus } from '@/components/ui/icons'

const statusColor = (status: string) => {
  switch (status) {
    case 'running': return 'default'
    case 'completed': return 'success'
    case 'failed': return 'destructive'
    case 'pending': return 'warning'
    default: return 'muted'
  }
}

export function TaskListPage() {
  const tasks = [
    { id: '1', name: '单细胞扰动预测', status: 'running', gpu_usage: 65, created_at: '2024-01-15' },
    { id: '2', name: 'RNA-seq 表达分析', status: 'completed', gpu_usage: 0, created_at: '2024-01-14' },
    { id: '3', name: '蛋白-配体预测', status: 'failed', gpu_usage: 0, created_at: '2024-01-13' },
    { id: '4', name: '基因组注释', status: 'pending', gpu_usage: 0, created_at: '2024-01-12' },
  ]

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-lg font-semibold">任务管理</CardTitle>
          <div className="flex items-center gap-2">
            <Button asChild>
              <a href="/tasks/create">
                <Plus size={18} strokeWidth={2} /> 新建任务
              </a>
            </Button>
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[30%]">任务名称</TableHead>
                  <TableHead className="w-[20%]">状态</TableHead>
                  <TableHead className="w-[15%]">GPU 使用</TableHead>
                  <TableHead className="w-[20%]">创建时间</TableHead>
                  <TableHead className="text-right w-[15%]">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tasks.map((task) => (
                  <TableRow key={task.id}>
                    <TableCell>{task.name}</TableCell>
                    <TableCell>
                      <Badge variant={statusColor(task.status)}>
                        {task.status}
                      </Badge>
                    </TableCell>
                    <TableCell>{task.gpu_usage}%</TableCell>
                    <TableCell>{task.created_at}</TableCell>
                    <TableCell className="text-right">
                      <Button variant="outline" size="sm" asChild>
                        <a href={`/tasks/${task.id}`}>详情</a>
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

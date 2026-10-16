import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Plus } from '@/components/ui/icons'
import { useTasksQuery } from '@/hooks/useTasks'
import { Link } from 'react-router-dom'

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
  const { data: tasks, isLoading, error } = useTasksQuery()

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-lg font-semibold">任务管理</CardTitle>
          <div className="flex items-center gap-2">
            <Button asChild>
              <Link to="/tasks/create">
                <Plus size={18} strokeWidth={2} /> 新建任务
              </Link>
            </Button>
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {isLoading ? (
            <div className="text-muted text-sm py-8 text-center">加载中…</div>
          ) : error ? (
            <div className="text-destructive text-sm py-8 text-center">
              加载任务列表失败：{error instanceof Error ? error.message : '未知错误'}
            </div>
          ) : !tasks?.length ? (
            <div className="text-muted text-sm py-8 text-center">暂无任务</div>
          ) : (
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
                          <Link to={`/tasks/${task.id}`}>详情</Link>
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

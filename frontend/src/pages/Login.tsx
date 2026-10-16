import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export function LoginPage() {
  return (
    <Card className="w-full max-w-md">
      <CardHeader className="space-y-1">
        <CardTitle className="text-h2">欢迎回来</CardTitle>
        <p className="text-sm text-muted">请登录 GPU 计算平台</p>
      </CardHeader>
      <CardContent>
        <form className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="username">用户名</Label>
            <Input id="username" placeholder="admin" />
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">密码</Label>
            <Input id="password" type="password" placeholder="••••••••" />
          </div>
          <Button type="submit" className="w-full">登录</Button>
        </form>
        <p className="text-xs text-muted text-center mt-4">
          没有账户？{' '}
          <a href="/register" className="text-primary hover:underline">
            立即注册
          </a>
        </p>
      </CardContent>
    </Card>
  )
}

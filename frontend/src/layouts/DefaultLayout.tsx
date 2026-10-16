'use client'

import { useState } from 'react'
import { Outlet, useNavigate, Link } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { Button } from '@/components/ui/button'
import {
  LayoutDashboard,
  List,
  Monitor,
  PieChart,
  Settings,
  LogOut,
} from '@/components/ui/icons'

const navItems = [
  { path: '/dashboard', label: '仪表盘', icon: LayoutDashboard },
  { path: '/tasks', label: '任务管理', icon: List },
  { path: '/gpu', label: 'GPU 监控', icon: Monitor },
  { path: '/cost', label: '成本优化', icon: PieChart },
  { path: '/settings', label: '系统设置', icon: Settings },
]

export function DefaultLayout() {
  const navigate = useNavigate()
  const { isLoggedIn, logout } = useAuth()
  const [sidebarOpen, setSidebarOpen] = useState(true)

  if (!isLoggedIn) {
    navigate('/login')
    return null
  }

  return (
    <div className="flex h-screen bg-background text-foreground">
      {/* 顶部标题栏（BioAI 风格 titlebar，43px） */}
      <header className="h-[43px] flex items-center justify-between flex-none px-4 border-b border-border bg-card user-select-none">
        <div className="brand flex items-center gap-2 font-semibold text-xs">
          <div className="brandmark h-5 w-5 rounded-md bg-primary grid place-items-center text-primary-foreground text-[10px] font-bold">
            G
          </div>
          <span>GPU<span className="font-medium text-muted-foreground">Panel</span></span>
        </div>
        <div className="titlecenter text-[10px] text-muted-foreground">
          GPU 计算平台 · 本地优先
        </div>
        <div className="title-actions flex items-center gap-2">
          <span className="text-[10px] text-muted-foreground flex items-center gap-1">
            <span className="status-ind h-1.5 w-1.5 rounded-full bg-green-400" />
            本地模式
          </span>
          <Button variant="ghost" size="icon" className="text-muted-foreground hover:text-accent">
            <span className="text-xs">⟳</span>
          </Button>
          <Button variant="ghost" size="icon" className="text-muted-foreground hover:text-accent">
            ☷
          </Button>
        </div>
      </header>

      <div className="flex-1 flex overflow-hidden">
        {/* 侧边栏 */}
        <aside
          className={`
            fixed left-0 top-[43px] bottom-0 w-[211px] border-r border-border bg-sidebar
            transition-transform duration-300 ease-in-out
            ${sidebarOpen ? 'translate-x-0' : '-translate-x-full'}
            z-50 flex flex-col px-2.5 py-3.5 gap-1
          `}
        >
          <div className="side-label text-[10px] uppercase tracking-widest text-muted-foreground font-semibold px-3 py-2 mb-1">
            Workspace
          </div>
          <nav className="space-y-0">
            {navItems.map((item) => {
              const Icon = item.icon
              const active = window.location.pathname === item.path
              return (
                <Link
                  key={item.path}
                  to={item.path}
                  onClick={() => setSidebarOpen(false)}
                  className={`
                    h-9 w-full flex items-center gap-3 rounded-md border-0 bg-transparent px-3 py-2.5 text-left font-medium
                    ${active
                      ? 'bg-accent-bg text-accent-dark'
                      : 'text-muted-foreground hover:bg-secondary hover:text-foreground'
                    }`}
                >
                  <Icon className="w-4 h-4 shrink-0" />
                  <span className="whitespace-nowrap">{item.label}</span>
                </Link>
              )
            })}
          </nav>
          <div className="side-bottom mt-auto border-t border-border pt-2 px-2">
            <Button
              variant="destructive"
              onClick={() => logout()}
              className="w-full text-xs h-8"
            >
              <LogOut className="w-3.5 h-3.5 mr-2" />
              退出登录
            </Button>
          </div>
        </aside>

        {sidebarOpen && (
          <div
            className="fixed inset-0 z-40 bg-black/30 lg:hidden"
            onClick={() => setSidebarOpen(false)}
          />
        )}

        {/* 主内容区 */}
        <div className="flex-1 flex flex-col overflow-hidden lg:pl-0 relative">
          <main className="flex-1 overflow-y-auto p-5 lg:p-6 lg:pb-8">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  )
}

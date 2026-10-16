import { Outlet } from 'react-router-dom'

export function AuthLayout() {
  return (
    <div className="flex h-screen w-full items-center justify-center bg-background">
      <div className="w-full max-w-md px-4">
        <Outlet />
      </div>
    </div>
  )
}

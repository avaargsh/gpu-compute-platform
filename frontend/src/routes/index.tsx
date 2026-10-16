import { createBrowserRouter } from 'react-router-dom';
import { DefaultLayout } from '@/layouts/DefaultLayout';
import { AuthLayout } from '@/layouts/AuthLayout';
import { DashboardPage } from '@/pages/Dashboard';
import { TaskListPage } from '@/pages/TaskList';
import { TaskCreatePage } from '@/pages/TaskCreate';
import { GPUMonitorPage } from '@/pages/GPUMonitor';
import { CostOptimizationPage } from '@/pages/CostOptimization';
import { SettingsPage } from '@/pages/Settings';
import { LoginPage } from '@/pages/Login';
import { NotFoundPage } from '@/pages/NotFound';

export const router = createBrowserRouter([
  {
    path: '/',
    element: <DefaultLayout />,
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'dashboard', element: <DashboardPage /> },
      { path: 'tasks', element: <TaskListPage /> },
      { path: 'tasks/create', element: <TaskCreatePage /> },
      { path: 'tasks/:id', element: <div>任务详情（建设中）</div> },
      { path: 'gpu', element: <GPUMonitorPage /> },
      { path: 'cost', element: <CostOptimizationPage /> },
      { path: 'settings', element: <SettingsPage /> },
    ],
  },
  {
    path: '/login',
    element: <AuthLayout />,
    children: [{ index: true, element: <LoginPage /> }],
  },
  {
    path: '/register',
    element: <AuthLayout />,
    children: [{ index: true, element: <div>注册页面（建设中）</div> }],
  },
  {
    path: '*',
    element: <NotFoundPage />,
  },
]);

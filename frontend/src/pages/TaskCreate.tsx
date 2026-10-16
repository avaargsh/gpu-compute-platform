'use client';

import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useNavigate } from 'react-router-dom';
import { useCreateTaskMutation } from '@/hooks/useTasks';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';

const schema = z.object({
  name: z.string().min(2, '名称至少 2 个字符'),
  provider: z.enum(['alibaba', 'tencent', 'runpod']),
  gpu_model: z.string().min(1, '请选择 GPU 型号'),
  image: z.string().min(1, '镜像不能为空'),
  script: z.string().min(1, '脚本路径不能为空'),
  scheduling_strategy: z.enum(['cost', 'performance', 'availability']),
  max_duration: z.number().optional().nullable(),
  budget_limit: z.number().optional().nullable(),
});

type FormData = z.infer<typeof schema>;

export function TaskCreatePage() {
  const navigate = useNavigate();
  const form = useForm<FormData>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: '',
      provider: 'tencent',
      gpu_model: '',
      image: '',
      script: '',
      scheduling_strategy: 'cost',
      max_duration: null,
      budget_limit: null,
    },
  });
  const createMutation = useCreateTaskMutation();

  const onSubmit = form.handleSubmit(async (data: FormData) => {
    try {
      await createMutation.mutateAsync(data);
      navigate('/tasks');
    } catch (err) {
      console.error(err);
    }
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>创建任务</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="col-span-2">
              <Label htmlFor="name">任务名称</Label>
              <Input id="name" {...form.register('name')} />
              {form.formState.errors.name && (
                <span className="text-destructive text-xs">{form.formState.errors.name.message}</span>
              )}
            </div>
            <div>
              <Label htmlFor="provider">云服务商</Label>
              <Select {...form.register('provider')}>
                <SelectTrigger>
                  <SelectValue placeholder="选择云服务商" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="alibaba">阿里云</SelectItem>
                  <SelectItem value="tencent">腾讯云</SelectItem>
                  <SelectItem value="runpod">RunPod</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label htmlFor="gpu_model">GPU 型号</Label>
              <Input id="gpu_model" {...form.register('gpu_model')} placeholder="如 V100, A100" />
            </div>
            <div>
              <Label htmlFor="image">Docker 镜像</Label>
              <Input id="image" {...form.register('image')} placeholder="pytorch/pytorch:latest" />
            </div>
          </div>
          <div className="col-span-2">
            <Label htmlFor="script">脚本路径</Label>
            <Input id="script" {...form.register('script')} placeholder="train/main.py" />
            {form.formState.errors.script && (
              <span className="text-destructive text-xs">{form.formState.errors.script.message}</span>
            )}
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <Label htmlFor="scheduling_strategy">调度策略</Label>
              <Select defaultValue="cost" {...form.register('scheduling_strategy')}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="cost">成本优先</SelectItem>
                  <SelectItem value="performance">性能优先</SelectItem>
                  <SelectItem value="availability">可用性优先</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label htmlFor="max_duration">最大时长（小时）</Label>
              <Input id="max_duration" type="number" {...form.register('max_duration')} />
            </div>
          </div>
          <Button type="submit" className="w-full" disabled={createMutation.isPending}>
            {createMutation.isPending ? '提交中...' : '提交任务'}
          </Button>
          {createMutation.isError && (
            <div className="text-sm text-destructive">{String(createMutation.error?.message)}</div>
          )}
        </form>
      </CardContent>
    </Card>
  );
}

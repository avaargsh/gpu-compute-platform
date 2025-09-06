import os
import logging
from typing import Dict, Any, Optional, List
import mlflow
import mlflow.tracking
from mlflow.tracking import MlflowClient
from app.core.config import settings

logger = logging.getLogger(__name__)


class MLflowManager:
    """MLflow管理器"""
    
    def __init__(self):
        """初始化MLflow配置"""
        # 设置tracking URI
        mlflow.set_tracking_uri(settings.mlflow_tracking_uri)
        self.client = MlflowClient(settings.mlflow_tracking_uri)
        self.experiment_name = settings.mlflow_experiment_name
        
        # 在测试环境下跳过MLflow初始化
        if os.getenv('TESTING') != 'true':
            # 确保实验存在
            self._ensure_experiment()
    
    def _ensure_experiment(self):
        """确保实验存在"""
        try:
            experiment = mlflow.get_experiment_by_name(self.experiment_name)
            if experiment is None:
                experiment_id = mlflow.create_experiment(
                    name=self.experiment_name,
                    artifact_location=settings.mlflow_artifact_location
                )
                logger.info(f"Created MLflow experiment: {self.experiment_name} (ID: {experiment_id})")
            else:
                logger.info(f"Using existing MLflow experiment: {self.experiment_name}")
        except Exception as e:
            logger.error(f"Failed to setup MLflow experiment: {e}")
    
    def start_run(self, 
                  task_id: str, 
                  run_name: Optional[str] = None,
                  tags: Optional[Dict[str, str]] = None) -> str:
        """启动MLflow运行"""
        try:
            # 设置实验
            mlflow.set_experiment(self.experiment_name)
            
            # 准备标签
            run_tags = {
                "task_id": task_id,
                "platform": "gpu-compute-platform",
            }
            if tags:
                run_tags.update(tags)
            
            # 启动运行
            run = mlflow.start_run(
                run_name=run_name or f"task-{task_id}",
                tags=run_tags
            )
            
            logger.info(f"Started MLflow run: {run.info.run_id} for task: {task_id}")
            return run.info.run_id
            
        except Exception as e:
            logger.error(f"Failed to start MLflow run for task {task_id}: {e}")
            raise
    
    def log_params(self, params: Dict[str, Any]):
        """记录参数"""
        try:
            for key, value in params.items():
                mlflow.log_param(key, value)
        except Exception as e:
            logger.error(f"Failed to log params: {e}")
    
    def log_metrics(self, metrics: Dict[str, float], step: Optional[int] = None):
        """记录指标"""
        try:
            for key, value in metrics.items():
                mlflow.log_metric(key, value, step=step)
        except Exception as e:
            logger.error(f"Failed to log metrics: {e}")
    
    def log_artifact(self, local_path: str, artifact_path: Optional[str] = None):
        """记录工件"""
        try:
            mlflow.log_artifact(local_path, artifact_path)
        except Exception as e:
            logger.error(f"Failed to log artifact: {e}")
    
    def log_text(self, text: str, artifact_file: str):
        """记录文本内容"""
        try:
            mlflow.log_text(text, artifact_file)
        except Exception as e:
            logger.error(f"Failed to log text: {e}")
    
    def set_tags(self, tags: Dict[str, str]):
        """设置标签"""
        try:
            for key, value in tags.items():
                mlflow.set_tag(key, value)
        except Exception as e:
            logger.error(f"Failed to set tags: {e}")
    
    def end_run(self, status: str = "FINISHED"):
        """结束运行"""
        try:
            mlflow.end_run(status=status)
        except Exception as e:
            logger.error(f"Failed to end MLflow run: {e}")
    
    def get_run(self, run_id: str) -> Optional[mlflow.entities.Run]:
        """获取运行信息"""
        try:
            return self.client.get_run(run_id)
        except Exception as e:
            logger.error(f"Failed to get run {run_id}: {e}")
            return None
    
    def search_runs(self, 
                   filter_string: str = "", 
                   max_results: int = 100) -> List[mlflow.entities.Run]:
        """搜索运行"""
        try:
            experiment = mlflow.get_experiment_by_name(self.experiment_name)
            if experiment:
                return self.client.search_runs(
                    experiment_ids=[experiment.experiment_id],
                    filter_string=filter_string,
                    max_results=max_results
                )
        except Exception as e:
            logger.error(f"Failed to search runs: {e}")
        return []


# 全局MLflow管理器实例（延迟初始化，避免在导入时连接外部服务）
_mlflow_manager: Optional[MLflowManager] = None


def get_mlflow_manager() -> MLflowManager:
    """获取MLflow管理器实例（首次调用时初始化）"""
    global _mlflow_manager
    if _mlflow_manager is None:
        _mlflow_manager = MLflowManager()
    return _mlflow_manager


class MLflowTaskTracker:
    """任务级别的MLflow追踪器"""
    
    def __init__(self, task_id: str, task_name: str, provider_name: str):
        self.task_id = task_id
        self.task_name = task_name
        self.provider_name = provider_name
        self.run_id = None
        self.mlflow_manager = get_mlflow_manager()
    
    def __enter__(self):
        """进入上下文管理器"""
        try:
            # 启动MLflow运行
            tags = {
                "provider": self.provider_name,
                "task_name": self.task_name,
            }
            
            self.run_id = self.mlflow_manager.start_run(
                task_id=self.task_id,
                run_name=f"{self.provider_name}-{self.task_name}",
                tags=tags
            )
            
        except Exception as e:
            logger.error(f"Failed to start MLflow tracking: {e}")
        
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        """退出上下文管理器"""
        try:
            if exc_type is not None:
                # 如果有异常，标记为失败
                self.mlflow_manager.set_tags({"status": "FAILED"})
                self.mlflow_manager.end_run(status="FAILED")
            else:
                self.mlflow_manager.set_tags({"status": "FINISHED"})
                self.mlflow_manager.end_run(status="FINISHED")
        except Exception as e:
            logger.error(f"Failed to end MLflow tracking: {e}")
    
    def log_job_config(self, job_config: Dict[str, Any]):
        """记录作业配置"""
        try:
            # 记录参数
            params = {
                "provider": self.provider_name,
                "gpu_type": job_config.get("gpu_spec", {}).get("gpu_type"),
                "gpu_count": job_config.get("gpu_spec", {}).get("gpu_count"),
                "vcpus": job_config.get("gpu_spec", {}).get("vcpus"),
                "ram_gb": job_config.get("gpu_spec", {}).get("ram_gb"),
                "image": job_config.get("image"),
                "timeout_minutes": job_config.get("timeout_minutes"),
            }
            
            # 过滤None值
            params = {k: v for k, v in params.items() if v is not None}
            self.mlflow_manager.log_params(params)
            
            # 记录完整配置作为工件
            import json
            config_json = json.dumps(job_config, indent=2, default=str)
            self.mlflow_manager.log_text(config_json, "job_config.json")
            
        except Exception as e:
            logger.error(f"Failed to log job config: {e}")
    
    def log_execution_metrics(self, 
                            duration_seconds: Optional[int] = None,
                            cost: Optional[float] = None,
                            exit_code: Optional[int] = None):
        """记录执行指标"""
        try:
            metrics = {}
            
            if duration_seconds is not None:
                metrics["duration_seconds"] = float(duration_seconds)
                metrics["duration_minutes"] = float(duration_seconds / 60)
            
            if cost is not None:
                metrics["cost"] = cost
            
            if exit_code is not None:
                metrics["exit_code"] = float(exit_code)
                metrics["success"] = 1.0 if exit_code == 0 else 0.0
            
            if metrics:
                self.mlflow_manager.log_metrics(metrics)
                
        except Exception as e:
            logger.error(f"Failed to log execution metrics: {e}")
    
    def log_task_logs(self, logs: str, max_length: int = 10000):
        """记录任务日志"""
        try:
            # 截断过长的日志
            if len(logs) > max_length:
                logs = logs[-max_length:] + "\n[... truncated ...]"
            
            self.mlflow_manager.log_text(logs, "task_logs.txt")
            
        except Exception as e:
            logger.error(f"Failed to log task logs: {e}")
    
    def log_error(self, error_message: str):
        """记录错误信息"""
        try:
            self.mlflow_manager.log_text(error_message, "error.txt")
            self.mlflow_manager.set_tags({"error": "true"})
            
        except Exception as e:
            logger.error(f"Failed to log error: {e}")

-- Initialize databases for GPU Compute Platform

-- Create database for MLflow if not exists
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'mlflow') THEN
        PERFORM dblink_exec('dbname=' || current_database(), 'CREATE DATABASE mlflow');
    END IF;
END
$$;

-- Grant permissions
GRANT ALL PRIVILEGES ON DATABASE gpu_platform TO postgres;
GRANT ALL PRIVILEGES ON DATABASE mlflow TO postgres;

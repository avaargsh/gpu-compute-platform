# Testing Guide for GPU Compute Platform

This document provides comprehensive guidance for testing the GPU Compute Platform, especially the GPU provider adapters.

## Test Structure

### Core Test Files

1. **`tests/test_gpu_providers.py`** - Basic GPU provider interface tests
   - Interface validation (JobConfig, GpuSpec, etc.)
   - Basic adapter initialization and configuration
   - Simple provider operations (submit_job, get_status, etc.)
   - Exception handling

2. **`tests/test_gpu_comprehensive.py`** - Comprehensive GPU provider tests
   - Edge cases and error scenarios
   - Complex job configurations
   - Provider switching scenarios
   - Integration testing patterns
   - Concurrent operations

3. **`tests/test_auth.py`** - Authentication system tests
4. **`tests/test_models.py`** - Database model tests
5. **`tests/test_api_basic.py`** - Basic API endpoint tests

## Running Tests

### Quick Test Commands

```bash
# Run all tests
uv run pytest

# Run with verbose output
uv run pytest -v

# Run only GPU provider tests
uv run pytest tests/test_gpu_providers.py -v

# Run comprehensive GPU tests
uv run pytest tests/test_gpu_comprehensive.py -v

# Run specific test by name
uv run pytest -k "test_submit_job_success" -v

# Run tests with coverage
uv run pytest --cov=app --cov-report=html
```

### Test Categories

#### 1. Unit Tests
- Test individual components in isolation
- Mock external dependencies (cloud APIs, Kubernetes)
- Fast execution, no external dependencies

#### 2. Integration Tests
- Test interaction between components
- Use real database connections (SQLite for testing)
- Test API endpoints with authentication

#### 3. Provider-Specific Tests

**Alibaba Cloud Adapter Tests:**
- ECS instance type mapping
- User data script generation
- Instance lifecycle management
- Error handling for API failures

**Tencent Cloud Adapter Tests:**
- Kubernetes job specification creation
- GPU resource calculations
- Job status mapping from Kubernetes
- Concurrent job submissions

## Test Configuration

### Environment Variables
For real integration testing (not included in CI), you can set:

```bash
# Alibaba Cloud
export ALIBABA_ACCESS_KEY_ID="your_access_key"
export ALIBABA_ACCESS_KEY_SECRET="your_secret_key"

# Tencent Cloud  
export TENCENT_SECRET_ID="your_secret_id"
export TENCENT_SECRET_KEY="your_secret_key"
```

### Test Database
Tests use an in-memory SQLite database that's created and destroyed for each test session.

## Writing New Tests

### Test Patterns

#### 1. Provider Adapter Tests
```python
def test_new_feature(self, provider_config):
    \"\"\"Test description.\"\"\"
    with patch('app.gpu.providers.provider.SomeClient'):
        adapter = ProviderAdapter(provider_config)
        
        # Test implementation
        result = adapter.some_method()
        
        # Assertions
        assert result is not None
```

#### 2. Async Tests
```python
@pytest.mark.asyncio
async def test_async_operation(self):
    \"\"\"Test async operations.\"\"\"
    result = await some_async_function()
    assert result == expected_value
```

#### 3. Exception Testing
```python
def test_error_handling(self):
    \"\"\"Test error scenarios.\"\"\"
    with pytest.raises(SpecificException) as exc_info:
        error_causing_function()
    
    assert exc_info.value.error_code == "EXPECTED_CODE"
```

### Mocking Guidelines

#### External APIs
Always mock external API calls to prevent:
- Network dependencies in tests
- Accidental charges from cloud providers
- Test flakiness from external service issues

```python
with patch('app.gpu.providers.alibaba.EcsClient') as mock_client:
    # Configure mock behavior
    mock_client.return_value.some_method.return_value = mock_response
    
    # Run test
    result = function_under_test()
```

#### Kubernetes Client
```python
with patch('app.gpu.providers.tencent.client') as mock_k8s_client:
    # Mock Kubernetes objects
    mock_job = Mock()
    mock_job.metadata.name = "test-job"
    mock_k8s_client.V1Job.return_value = mock_job
```

## Test Data

### Sample Configurations

#### Alibaba Cloud Config
```python
alibaba_config = {
    "access_key_id": "test_access_key",
    "access_key_secret": "test_secret", 
    "region_id": "cn-hangzhou",
    "security_group_id": "sg-test123",
    "vswitch_id": "vsw-test123"
}
```

#### Tencent Cloud Config
```python
tencent_config = {
    "secret_id": "test_secret_id",
    "secret_key": "test_secret_key", 
    "region": "ap-shanghai",
    "cluster_id": "cls-test123",
    "kubeconfig": "base64_encoded_config"
}
```

#### GPU Specifications
```python
# Single GPU
gpu_spec = GpuSpec(
    gpu_type="A100",
    gpu_count=1,
    memory_gb=40,
    vcpus=12,
    ram_gb=48
)

# Multi-GPU
multi_gpu_spec = GpuSpec(
    gpu_type="V100", 
    gpu_count=4,
    memory_gb=128,
    vcpus=32,
    ram_gb=128
)
```

## Troubleshooting Tests

### Common Issues

#### 1. Import Errors
- Ensure all dependencies are installed: `uv sync`
- Check Python path and module structure

#### 2. Mock Configuration
- Verify mock paths match actual import structure
- Use `patch.object()` for more precise mocking

#### 3. Async Test Issues
- Always use `@pytest.mark.asyncio` for async tests
- Check that async functions are properly awaited

#### 4. Database Tests
- Ensure database is properly initialized in test setup
- Use `conftest.py` for shared fixtures

### Debug Commands
```bash
# Run with debugging
uv run pytest --pdb

# Show all print statements
uv run pytest -s

# Run only failed tests from last run
uv run pytest --lf

# Show local variables in tracebacks
uv run pytest --tb=long -l
```

## Coverage Requirements

- Aim for >90% code coverage on core modules
- Focus on edge cases and error paths
- Ensure all provider interface methods are tested
- Include both success and failure scenarios

## CI/CD Integration

Tests are configured to run automatically on:
- Pull requests
- Main branch pushes
- Release tags

All tests must pass before code can be merged.

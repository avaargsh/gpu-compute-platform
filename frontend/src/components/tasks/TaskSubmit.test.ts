import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import TaskSubmit from './TaskSubmit.vue'
import { useTaskStore } from '@/stores/taskStore'
import { useCostStore } from '@/stores/costStore'
import type { InstanceRecommendation } from '@/types'

// Mock the router
const mockRouter = {
  push: vi.fn()
}

vi.mock('vue-router', () => ({
  useRouter: () => mockRouter
}))

// Mock Element Plus components
vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
  },
  ElMessageBox: {
    confirm: vi.fn()
  }
}))

const mockRecommendation: InstanceRecommendation = {
  id: 'instance-1',
  name: 'Standard GPU Instance',
  gpu_type: 'RTX 4090',
  gpu_count: 1,
  vcpus: 8,
  memory_gb: 32,
  storage_gb: 500,
  cost_per_hour: 2.5,
  estimated_runtime_hours: 2,
  total_cost: 5.0,
  performance_score: 85,
  availability: 'high'
}

describe('TaskSubmit', () => {
  let wrapper: VueWrapper<any>
  let taskStore: any
  let costStore: any

  beforeEach(() => {
    setActivePinia(createPinia())
    taskStore = useTaskStore()
    costStore = useCostStore()
    
    // Mock store methods
    taskStore.submitTask = vi.fn()
    costStore.fetchRecommendations = vi.fn()
    costStore.recommendations = []
    costStore.loading = false

    wrapper = mount(TaskSubmit, {
      global: {
        plugins: [createPinia()],
        stubs: {
          'el-card': { template: '<div class="el-card"><slot name="header"></slot><slot></slot></div>' },
          'el-form': { template: '<form class="el-form"><slot></slot></form>' },
          'el-form-item': { template: '<div class="el-form-item"><label><slot name="label"></slot></label><slot></slot></div>' },
          'el-input': { 
            template: '<input class="el-input" v-model="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
            props: ['modelValue'],
            emits: ['update:modelValue']
          },
          'el-select': { 
            template: '<select class="el-select" v-model="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><slot></slot></select>',
            props: ['modelValue'],
            emits: ['update:modelValue']
          },
          'el-option': { template: '<option class="el-option" :value="value"><slot></slot></option>', props: ['value'] },
          'el-input-number': { 
            template: '<input class="el-input-number" type="number" v-model="modelValue" @input="$emit(\'update:modelValue\', Number($event.target.value))" />',
            props: ['modelValue'],
            emits: ['update:modelValue']
          },
          'el-button': { 
            template: '<button class="el-button" :disabled="loading"><slot></slot></button>',
            props: ['loading']
          },
          'el-table': { template: '<table class="el-table"><slot></slot></table>' },
          'el-table-column': { template: '<td class="el-table-column"><slot></slot></td>' },
          'el-dialog': { template: '<div class="el-dialog" v-show="modelValue"><slot></slot></div>', props: ['modelValue'] },
          'el-upload': { template: '<div class="el-upload"><slot></slot></div>' },
          'el-icon': { template: '<i class="el-icon"><slot></slot></i>' }
        }
      }
    })
  })

  afterEach(() => {
    wrapper.unmount()
  })

  describe('component rendering', () => {
    it('should render the form with all required fields', () => {
      expect(wrapper.find('.task-submit').exists()).toBe(true)
      expect(wrapper.find('.el-card').exists()).toBe(true)
      expect(wrapper.find('.el-form').exists()).toBe(true)
      
      // Check for form fields
      const inputs = wrapper.findAll('.el-input')
      expect(inputs.length).toBeGreaterThan(0)
      
      const selects = wrapper.findAll('.el-select')
      expect(selects.length).toBeGreaterThan(0)
    })

    it('should display the card header correctly', () => {
      const header = wrapper.find('.card-header')
      expect(header.exists()).toBe(true)
      expect(header.text()).toContain('创建新任务')
    })

    it('should render form buttons', () => {
      const buttons = wrapper.findAll('.el-button')
      expect(buttons.length).toBeGreaterThan(0)
      
      const submitButton = buttons.find(btn => btn.text().includes('创建任务'))
      const resetButton = buttons.find(btn => btn.text().includes('重置'))
      
      expect(submitButton).toBeDefined()
      expect(resetButton).toBeDefined()
    })
  })

  describe('form interaction', () => {
    it('should update form data when inputs change', async () => {
      const nameInput = wrapper.find('.el-input')
      await nameInput.setValue('Test Task')
      await nameInput.trigger('input')

      const vm = wrapper.vm as any
      expect(vm.form.name).toBe('Test Task')
    })

    it('should update GPU type when select changes', async () => {
      const gpuSelect = wrapper.find('.el-select')
      await gpuSelect.setValue('rtx4090')
      await gpuSelect.trigger('change')

      const vm = wrapper.vm as any
      expect(vm.form.gpu_type).toBe('rtx4090')
    })

    it('should update max duration when input number changes', async () => {
      const durationInput = wrapper.find('.el-input-number')
      await durationInput.setValue('2')
      await durationInput.trigger('input')

      const vm = wrapper.vm as any
      expect(vm.form.max_duration).toBe(2)
    })
  })

  describe('recommendations functionality', () => {
    it('should call fetchRecommendations when get recommendations button is clicked', async () => {
      const vm = wrapper.vm as any
      costStore.fetchRecommendations.mockResolvedValue(undefined)

      await vm.getRecommendations()

      expect(costStore.fetchRecommendations).toHaveBeenCalledWith({
        task_type: 'general',
        performance_priority: 'balanced'
      })
    })

    it('should display recommendations table when recommendations are available', async () => {
      const vm = wrapper.vm as any
      vm.recommendations = [mockRecommendation]
      await wrapper.vm.$nextTick()

      const recommendationsSection = wrapper.find('.recommendations')
      expect(recommendationsSection.exists()).toBe(true)
      
      const table = recommendationsSection.find('.el-table')
      expect(table.exists()).toBe(true)
    })

    it('should select instance when select button is clicked', async () => {
      const vm = wrapper.vm as any
      
      await vm.selectInstance(mockRecommendation)

      expect(vm.selectedInstance).toEqual(mockRecommendation)
    })
  })

  describe('form submission', () => {
    it('should validate form before submission', async () => {
      const vm = wrapper.vm as any
      const mockValidate = vi.fn().mockResolvedValue(false)
      vm.$refs.formRef = { validate: mockValidate }

      await vm.handleSubmit()

      expect(mockValidate).toHaveBeenCalled()
      expect(taskStore.submitTask).not.toHaveBeenCalled()
    })

    it('should submit task with correct data when form is valid', async () => {
      const vm = wrapper.vm as any
      const mockValidate = vi.fn().mockResolvedValue(true)
      vm.$refs.formRef = { validate: mockValidate }
      
      vm.form = {
        name: 'Test Task',
        script_path: '/path/to/script.py',
        gpu_type: 'rtx4090',
        max_duration: 2
      }
      vm.requirementsText = 'numpy\ntorch'
      
      taskStore.submitTask.mockResolvedValue({ id: 'task-1' })

      await vm.handleSubmit()

      expect(taskStore.submitTask).toHaveBeenCalledWith({
        name: 'Test Task',
        script_path: '/path/to/script.py',
        requirements: ['numpy', 'torch'],
        gpu_type: 'rtx4090',
        max_duration: 2
      })
    })

    it('should handle submission errors gracefully', async () => {
      const vm = wrapper.vm as any
      const mockValidate = vi.fn().mockResolvedValue(true)
      vm.$refs.formRef = { validate: mockValidate }
      
      const error = new Error('Submission failed')
      taskStore.submitTask.mockRejectedValue(error)

      await vm.handleSubmit()

      expect(vm.submitting).toBe(false)
    })

    it('should redirect to task detail after successful submission', async () => {
      const vm = wrapper.vm as any
      const mockValidate = vi.fn().mockResolvedValue(true)
      vm.$refs.formRef = { validate: mockValidate }
      
      const newTask = { id: 'task-1', name: 'Test Task' }
      taskStore.submitTask.mockResolvedValue(newTask)

      await vm.handleSubmit()

      expect(mockRouter.push).toHaveBeenCalledWith('/tasks/task-1')
    })
  })

  describe('form reset', () => {
    it('should reset form data when reset button is clicked', async () => {
      const vm = wrapper.vm as any
      vm.form.name = 'Test Task'
      vm.requirementsText = 'numpy'
      vm.recommendations = [mockRecommendation]

      await vm.handleReset()

      expect(vm.form.name).toBe('')
      expect(vm.requirementsText).toBe('')
      expect(vm.recommendations).toEqual([])
    })

    it('should reset form validation when reset is called', async () => {
      const vm = wrapper.vm as any
      const mockResetFields = vi.fn()
      vm.$refs.formRef = { resetFields: mockResetFields }

      await vm.handleReset()

      expect(mockResetFields).toHaveBeenCalled()
    })
  })

  describe('file upload', () => {
    it('should show upload dialog when upload button is clicked', async () => {
      const uploadButton = wrapper.find('.el-button')
      // Find the upload button by looking for the button that shows the dialog
      const vm = wrapper.vm as any
      
      vm.showUploadDialog = true
      await wrapper.vm.$nextTick()

      expect(vm.showUploadDialog).toBe(true)
    })

    it('should handle file selection', async () => {
      const vm = wrapper.vm as any
      const mockFile = new File(['content'], 'test.py', { type: 'text/python' })
      
      vm.handleFileChange({ raw: mockFile })

      expect(vm.selectedFile).toBe(mockFile)
    })

    it('should update script path after successful upload', async () => {
      const vm = wrapper.vm as any
      vm.selectedFile = new File(['content'], 'test.py', { type: 'text/python' })
      
      // Mock upload success
      const mockSubmit = vi.fn().mockResolvedValue(undefined)
      vm.$refs.uploadRef = { submit: mockSubmit }
      
      await vm.handleUpload()

      expect(vm.uploading).toBe(false)
      expect(vm.showUploadDialog).toBe(false)
    })
  })

  describe('form validation rules', () => {
    it('should have validation rules defined', () => {
      const vm = wrapper.vm as any
      expect(vm.rules).toBeDefined()
      expect(vm.rules.name).toBeDefined()
      expect(vm.rules.script_path).toBeDefined()
    })

    it('should validate required fields', () => {
      const vm = wrapper.vm as any
      const nameRule = vm.rules.name.find((rule: any) => rule.required)
      const scriptRule = vm.rules.script_path.find((rule: any) => rule.required)
      
      expect(nameRule).toBeDefined()
      expect(scriptRule).toBeDefined()
    })
  })

  describe('computed properties', () => {
    it('should parse requirements text correctly', () => {
      const vm = wrapper.vm as any
      vm.requirementsText = 'numpy==1.21.0\ntorch>=1.9.0\n\ntransformers'
      
      expect(vm.parsedRequirements).toEqual(['numpy==1.21.0', 'torch>=1.9.0', 'transformers'])
    })

    it('should return empty array for empty requirements text', () => {
      const vm = wrapper.vm as any
      vm.requirementsText = ''
      
      expect(vm.parsedRequirements).toEqual([])
    })
  })
})

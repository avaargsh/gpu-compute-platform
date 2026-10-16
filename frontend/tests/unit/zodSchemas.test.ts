import { describe, it, expect } from 'vitest'
import { z } from 'zod'

// 与 Login.tsx 中相同的 schema
const loginSchema = z.object({
  username: z.string().min(1, '用户名不能为空'),
  password: z.string().min(1, '密码不能为空'),
})

describe('login Zod schema', () => {
  it('should accept valid credentials', () => {
    const result = loginSchema.safeParse({ username: 'admin', password: 'secret' })
    expect(result.success).toBe(true)
  })

  it('should reject empty username', () => {
    const result = loginSchema.safeParse({ username: '', password: 'secret' })
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.errors[0].message).toBe('用户名不能为空')
    }
  })

  it('should reject empty password', () => {
    const result = loginSchema.safeParse({ username: 'admin', password: '' })
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.errors[0].message).toBe('密码不能为空')
    }
  })

  it('should reject both empty', () => {
    const result = loginSchema.safeParse({ username: '', password: '' })
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.errors.length).toBe(2)
    }
  })
})

'use client'

import { useCallback, useState } from 'react'
import {
  rememberOperation,
  recoverOperation,
  forgetOperation,
  type OperationPointer,
} from '@/management/recovery'

export interface UseOperationReturn {
  pointer: OperationPointer | null
  setPointer: (pointer: OperationPointer | null) => void
  remember: (scope: { cluster: string; project: string; kind: 'ComputePool' | 'Workload'; resource: string }, method: 'PUT' | 'DELETE', operationId: string) => OperationPointer
  recover: () => OperationPointer | null
  forget: () => void
}

export function useOperation(): UseOperationReturn {
  const [pointer, setPointer] = useState<OperationPointer | null>(null)

  const remember = useCallback(
    (scope, method, operationId) => {
      const p = rememberOperation(sessionStorage, scope, method, operationId)
      setPointer(p)
      return p
    },
    []
  )

  const recover = useCallback(() => {
    const p = recoverOperation(sessionStorage)
    setPointer(p)
    return p
  }, [])

  const forget = useCallback(() => {
    forgetOperation(sessionStorage)
    setPointer(null)
  }, [])

  return {
    pointer,
    setPointer,
    remember,
    recover,
    forget,
  }
}

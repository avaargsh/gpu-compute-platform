'use client'

import { useCallback, useState } from 'react'
import {
  rememberOperation,
  recoverOperation,
  hasUnresolvedRecoveryRecord,
  retireOperation,
  forgetOperation,
  type OperationPointer,
} from '@/management/recovery'

export interface UseOperationReturn {
  pointer: OperationPointer | null
  recoveryBlocked: boolean
  setPointer: (pointer: OperationPointer | null) => void
  remember: (scope: { cluster: string; project: string; kind: 'ComputePool' | 'Workload'; resource: string }, method: 'PUT' | 'DELETE', operationId: string) => OperationPointer
  recover: () => OperationPointer | null
  retire: (scope: { cluster: string; project: string; kind: 'ComputePool' | 'Workload'; resource: string }, method: 'PUT' | 'DELETE', operationId: string) => boolean
  forget: () => void
}

export function useOperation(): UseOperationReturn {
  const [pointer, setPointer] = useState<OperationPointer | null>(null)
  const [recoveryBlocked, setRecoveryBlocked] = useState(false)

  const remember = useCallback(
    (scope: { cluster: string; project: string; kind: 'ComputePool' | 'Workload'; resource: string }, method: 'PUT' | 'DELETE', operationId: string) => {
      const p = rememberOperation(sessionStorage, scope, method, operationId)
      setPointer(p)
      setRecoveryBlocked(false)
      return p
    },
    []
  )

  const recover = useCallback(() => {
    const p = recoverOperation(sessionStorage)
    setPointer(p)
    setRecoveryBlocked(hasUnresolvedRecoveryRecord(sessionStorage))
    return p
  }, [])

  const retire = useCallback(
    (scope: { cluster: string; project: string; kind: 'ComputePool' | 'Workload'; resource: string }, method: 'PUT' | 'DELETE', operationId: string) => {
      const removed = retireOperation(sessionStorage, scope, method, operationId)
      if (removed) {
        setPointer(null)
        setRecoveryBlocked(false)
      }
      return removed
    }, []
  )

  const forget = useCallback(() => {
    forgetOperation(sessionStorage)
    setPointer(null)
    setRecoveryBlocked(false)
  }, [])

  return {
    pointer,
    recoveryBlocked,
    setPointer,
    remember,
    recover,
    retire,
    forget,
  }
}

import React from 'react'
import { createRoot } from 'react-dom/client'
import { ManagementApp } from './Management'
import '../index.css'

const root = createRoot(
  document.getElementById('root') as HTMLElement
)

root.render(
  <React.StrictMode>
    <ManagementApp />
  </React.StrictMode>
)

import React from 'react'
import { createRoot } from 'react-dom/client'
import { PortalApp } from './Portal'
import '../index.css'
import './style.css'

const root = createRoot(
  document.getElementById('root') as HTMLElement
)

root.render(
  <React.StrictMode>
    <PortalApp />
  </React.StrictMode>
)

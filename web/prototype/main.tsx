import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { PrototypeApp } from './PrototypeApp'
import './styles.css'

createRoot(document.getElementById('root')!).render(<StrictMode><PrototypeApp /></StrictMode>)

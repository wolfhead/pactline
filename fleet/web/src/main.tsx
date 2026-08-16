import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { App } from './App'
import { LiveObservationProvider } from './data'
import './styles.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <LiveObservationProvider><App /></LiveObservationProvider>
    </BrowserRouter>
  </React.StrictMode>,
)

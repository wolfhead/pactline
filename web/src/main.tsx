import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { IdentityProvider } from './identity'
// index.css imports styles.css itself, into a `legacy` layer below Tailwind's
// utilities — see the note there. Importing it again here would reintroduce
// an unlayered copy that outranks every utility class.
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <IdentityProvider>
        <App />
      </IdentityProvider>
    </BrowserRouter>
  </React.StrictMode>,
)

import { NavLink, Route, Routes } from 'react-router-dom'
import { UserSwitcher } from './identity'
import WorkFeed from './pages/WorkFeed'
import Board from './pages/Board'
import BountyDetail from './pages/BountyDetail'
import Portfolio from './pages/Portfolio'
import Mine from './pages/Mine'

export default function App() {
  return (
    <div className="app">
      <header>
        <nav>
          <NavLink to="/">作品流</NavLink>
          <NavLink to="/board">Board</NavLink>
          <NavLink to="/mine">我的</NavLink>
        </nav>
        <UserSwitcher />
      </header>
      <main>
        <Routes>
          <Route path="/" element={<WorkFeed />} />
          <Route path="/board" element={<Board />} />
          <Route path="/bounties/:id" element={<BountyDetail />} />
          <Route path="/users/:id/portfolio" element={<Portfolio />} />
          <Route path="/mine" element={<Mine />} />
        </Routes>
      </main>
    </div>
  )
}

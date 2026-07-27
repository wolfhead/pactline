import { NavLink, Route, Routes } from 'react-router-dom'
import { UserSwitcher, useIdentity } from './identity'
import WorkFeed from './pages/WorkFeed'
import Board from './pages/Board'
import BountyDetail from './pages/BountyDetail'
import Portfolio from './pages/Portfolio'
import Mine from './pages/Mine'
import Steward from './pages/Steward'

export default function App() {
  const { me } = useIdentity()
  const isSteward = Boolean(me?.roles.includes('STEWARD'))

  return (
    <div className="app">
      <header>
        <nav>
          <NavLink to="/">作品流</NavLink>
          <NavLink to="/board">Board</NavLink>
          <NavLink to="/mine">我的</NavLink>
          {/* Settlement and the anchor list are steward-only tools with no
              per-bounty scope, so they get their own nav entry rather than
              living on a bounty page — visible only to a steward, since
              nobody else can act on it anyway (spec §7.2, §4.7). */}
          {isSteward && <NavLink to="/steward">Steward</NavLink>}
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
          <Route path="/steward" element={<Steward />} />
        </Routes>
      </main>
    </div>
  )
}

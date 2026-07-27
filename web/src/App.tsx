import { Route, Routes } from 'react-router-dom'
import { UserSwitcher } from './identity'
import WorkFeed from './legacy/pages/WorkFeed'
import Board from './legacy/pages/Board'
import BountyDetail from './legacy/pages/BountyDetail'
import Portfolio from './legacy/pages/Portfolio'
import Mine from './legacy/pages/Mine'
import Steward from './legacy/pages/Steward'

export default function App() {
  // The bounty/credit/scoring mechanism (work feed, board, portfolio, mine,
  // steward tools) moved to internal/legacy on both backend and frontend —
  // see web/src/legacy/README.md. Its routes stay mounted below so the pages
  // remain reachable by direct URL (and drivable by the Playwright suite),
  // but they are deliberately no longer linked from the nav: the plain
  // routes are reserved for the task system that replaces this mechanism.
  return (
    <div className="app">
      <header>
        <nav></nav>
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

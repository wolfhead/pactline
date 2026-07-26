import { useParams } from 'react-router-dom'

export default function BountyDetail() {
  const { id } = useParams()
  return (
    <div className="card">
      <h3>单详情</h3>
      <p className="hint">尚未实现（单号 {id}）— 由 Task 14 补齐。</p>
    </div>
  )
}

import { useParams } from 'react-router-dom'

export default function Portfolio() {
  const { id } = useParams()
  return (
    <div className="card">
      <h3>作品集</h3>
      <p className="hint">尚未实现（用户 {id}）— 由 Task 15 补齐。</p>
    </div>
  )
}

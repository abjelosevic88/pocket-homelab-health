import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { GoalView, Metric } from '../lib/types'
import { fmtMetric, fmtMinutes } from '../lib/units'
import { Empty, Loading, PageHeader, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Ring } from '../components/Chart'
import { GoalModal } from '../components/forms'
import { Select } from '../components/Select'
import { useCatalog, useOnChange } from '../components/Layout'

export default function Goals() {
  const catalog = useCatalog()
  const { toast } = useToast()
  const [goals, setGoals] = useState<GoalView[] | null>(null)
  const [pick, setPick] = useState('')
  const [edit, setEdit] = useState<{ metric: Metric; goal?: GoalView } | null>(null)
  const load = () => api.get<GoalView[]>(`${V1}/goals`).then(setGoals).catch(() => {})
  useEffect(() => { load() }, [])
  useOnChange(load)
  if (!goals) return <Loading />
  const candidates = (catalog?.metrics || []).filter(m => m.goal_dir && !goals.some(g => g.metric === m.key))
  const toggle = async (g: GoalView) => { try { await api.put(`${V1}/goals/${g.metric}`, { target: g.target, direction: g.direction, period: g.period, active: !g.active }); load() } catch (e) { toast((e as Error).message, 'err') } }
  const remove = async (g: GoalView) => { try { await api.del(`${V1}/goals/${g.metric}`); load() } catch (e) { toast((e as Error).message, 'err') } }
  const fmt = (g: GoalView, v: number) => g.metric.startsWith('sleep_') ? fmtMinutes(v) : fmtMetric(g.metric_info, v)
  return (
    <div className="stack loose">
      <PageHeader title="Goals" sub={`${goals.filter(g => g.met).length} of ${goals.filter(g => g.active).length} met today`} actions={
        <Select value={pick} onChange={v => { const m = candidates.find(x => x.key === v); if (m) setEdit({ metric: m }); setPick('') }} options={candidates.map(m => ({ value: m.key, label: m.name, group: m.category[0].toUpperCase() + m.category.slice(1) }))} placeholder="+ New goal" />
      } />
      {goals.length === 0 ? <Empty title="No goals yet" icon="target">Pick a metric above: at least 8 000 steps, at most 200 mg caffeine, under 80 kg…</Empty> : (
        <div className="goal-grid">
          {goals.map(g => (
            <section key={g.id} className={`card goal-card ${g.active ? '' : 'off'}`} style={{ '--tile': g.metric_info.color } as React.CSSProperties}>
              <div className="goal-top">
                <Ring pct={g.pct} size={64} stroke={7} color={g.metric_info.color}><Icon name={g.met ? 'check' : g.metric_info.icon} size={20} /></Ring>
                <div className="grow">
                  <Link to={`/metrics/${g.metric}`} className="goal-name">{g.metric_info.name}</Link>
                  <div className="muted small">{g.direction === 'at_most' ? 'At most' : 'At least'} {fmt(g, g.target)} a day</div>
                  <div className="goal-today tnum">{g.today !== null ? fmt(g, g.today) : '—'} <span className="muted small">today</span></div>
                </div>
              </div>
              <div className="goal-stats small">
                <span><Icon name="fire" size={13} />{g.streak} day streak</span>
                <span><Icon name="trophy" size={13} />best {g.best}</span>
                <span><Icon name="calendar" size={13} />{g.last30} of the last 30 days</span>
              </div>
              <div className="row wrap goal-actions">
                <button type="button" className="btn sm" onClick={() => setEdit({ metric: g.metric_info, goal: g })}>Edit</button>
                <button type="button" className="btn ghost sm" onClick={() => toggle(g)}>{g.active ? 'Pause' : 'Resume'}</button>
                <button type="button" className="btn ghost sm danger-text" onClick={() => remove(g)}>Remove</button>
              </div>
            </section>
          ))}
        </div>
      )}
      {edit && <GoalModal metric={edit.metric} goal={edit.goal} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); load() }} />}
    </div>
  )
}

import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { RANGES, type MetricPage, type SleepSession } from '../lib/types'
import { fmtDate, fmtISOTime, weekday } from '../lib/format'
import { fmtMinutes } from '../lib/units'
import { Confirm, Empty, Loading, PageHeader, Segmented, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { LineChart, Stacked } from '../components/Chart'
import { openCreate, useOnChange } from '../components/Layout'
import { fillDates } from './MetricPage'

export default function Sleep() {
  const [sp, setSp] = useSearchParams()
  const { toast } = useToast()
  const range = sp.get('range') || 'm'
  const [nights, setNights] = useState<SleepSession[] | null>(null)
  const [page, setPage] = useState<MetricPage | null>(null)
  const [del, setDel] = useState<SleepSession | null>(null)
  const load = () => {
    api.get<SleepSession[]>(`${V1}/sleep?range=${range}&limit=400`).then(setNights).catch(() => {})
    api.get<MetricPage>(`${V1}/metrics/sleep_asleep?range=${range}`).then(setPage).catch(() => {})
  }
  useEffect(() => { load() }, [range]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  if (!nights || !page) return <Loading />
  const avg = page.stats.avg
  const pts = fillDates(page.from, page.to, page.points.map(p => ({ x: p.date, y: p.value })))
  const deep = nights.length ? nights.reduce((a, n) => a + n.deep_s, 0) / nights.length / 60 : 0
  const rem = nights.length ? nights.reduce((a, n) => a + n.rem_s, 0) / nights.length / 60 : 0
  const remove = async (n: SleepSession) => { try { await api.del(`${V1}/sleep/${n.id}`); setDel(null); load() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <PageHeader title="Sleep" sub={nights.length ? `${nights.length} nights · ${fmtMinutes(avg)} asleep on average${deep ? ` · deep ${fmtMinutes(deep)}` : ''}${rem ? ` · REM ${fmtMinutes(rem)}` : ''}` : 'No nights yet'}
        actions={<button type="button" className="btn primary" onClick={() => openCreate('sleep')}><Icon name="plus" size={15} />Add a night</button>} />
      <div className="scroll-x"><Segmented size="sm" value={range} onChange={v => setSp({ range: v })} options={RANGES.map(r => ({ value: r.id, label: r.label }))} /></div>
      <section className="card"><div className="card-body">
        {page.points.length === 0 ? <p className="muted">No sleep in this range.</p> : <LineChart points={pts} kind="bar" color="#6366f1" goal={page.goal?.target} goalLabel={page.goal ? `goal ${fmtMinutes(page.goal.target)}` : undefined} format={v => fmtMinutes(v)} zero avg={avg} height={200} />}
      </div></section>
      {nights.length === 0 ? <Empty title="No nights recorded" icon="moon">Sleep arrives with the phone sync, or <button type="button" className="link-btn" onClick={() => openCreate('sleep')}>add one by hand</button>.</Empty> : (
        <section className="card"><div className="card-body flush">
          <ul className="list">{nights.map(n => (
            <li key={n.id} className="list-row night">
              <span className="night-date"><b>{weekday(n.date)}</b><span className="muted small block">{fmtDate(n.date, false)}</span></span>
              <span className="grow">
                <span className="row" style={{ gap: 8, alignItems: 'baseline' }}><b className="tnum">{fmtMinutes(n.asleep_s / 60)}</b><span className="muted small">{fmtISOTime(n.start_at)} → {fmtISOTime(n.end_at)} · in bed {fmtMinutes(n.in_bed_s / 60)}{n.awake_s ? ` · awake ${fmtMinutes(n.awake_s / 60)}` : ''}</span></span>
                {(n.deep_s || n.rem_s || n.core_s) ? <Stacked parts={[{ value: n.deep_s, color: '#4338ca', label: 'Deep' }, { value: n.core_s, color: '#818cf8', label: 'Core' }, { value: n.rem_s, color: '#c7d2fe', label: 'REM' }, { value: n.awake_s, color: '#f59e0b', label: 'Awake' }]} height={8} /> : null}
              </span>
              <span className="muted small">{n.device || n.source}</span>
              {n.source === 'manual' && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(n)}>Delete</button>}
            </li>
          ))}</ul>
        </div></section>
      )}
      {del && <Confirm title="Delete this night?" message={`${fmtDate(del.date)}: ${fmtMinutes(del.asleep_s / 60)} asleep`} confirmLabel="Delete" onCancel={() => setDel(null)} onConfirm={() => remove(del)} />}
    </div>
  )
}

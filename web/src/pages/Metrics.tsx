import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Category, MetricView } from '../lib/types'
import { fmtDate } from '../lib/format'
import { fmtMetric, fmtPair } from '../lib/units'
import { Loading, PageHeader, Segmented, Switch } from '../components/ui'
import { Icon } from '../components/icons'
import { Sparkline } from '../components/Chart'
import { openCreate, useCatalog, useOnChange } from '../components/Layout'

export default function Metrics() {
  const catalog = useCatalog()
  const [all, setAll] = useState(false)
  const [cat, setCat] = useState<Category | 'all'>('all')
  const [list, setList] = useState<MetricView[] | null>(null)
  const load = () => api.get<MetricView[]>(`${V1}/metrics${all ? '?all=1' : ''}`).then(setList).catch(() => {})
  useEffect(() => { load() }, [all]) // eslint-disable-line react-hooks/exhaustive-deps
  useOnChange(load)
  if (!list) return <Loading />
  const cats = catalog?.categories || []
  const shown = list.filter(m => cat === 'all' || m.category === cat)
  const groups = cats.map(c => ({ ...c, items: shown.filter(m => m.category === c.key) })).filter(g => g.items.length)
  return (
    <div className="stack loose">
      <PageHeader title="Metrics" sub={`${list.filter(m => m.days > 0).length} with data`} actions={<>
        <label className="check-row small"><Switch checked={all} onChange={setAll} label="Show every metric" size="sm" /><span>Show all</span></label>
        <button type="button" className="btn primary" onClick={() => openCreate('log')}><Icon name="plus" size={15} />Log</button>
      </>} />
      <div className="scroll-x"><Segmented size="sm" value={cat} onChange={v => setCat(v as Category | 'all')} options={[{ value: 'all', label: 'All' }, ...cats.filter(c => list.some(m => m.category === c.key)).map(c => ({ value: c.key, label: c.name }))]} /></div>
      {groups.length === 0 && <p className="muted">Nothing yet. Connect your phone under Settings → Phone & imports, or switch on “Show all” and log something.</p>}
      {groups.map(g => (
        <section key={g.key} className="stack">
          <h3 className="group-h">{g.name}</h3>
          <div className="metric-grid">
            {g.items.map(m => (
              <Link key={m.key} to={`/metrics/${m.key}`} className={`metric-card ${m.days ? '' : 'empty'}`} style={{ '--tile': m.color } as React.CSSProperties}>
                <span className="tile-ico"><Icon name={m.icon} size={16} /></span>
                <span className="grow">
                  <span className="metric-name">{m.name}</span>
                  <span className="metric-val">{m.days ? (m.pair ? fmtPair(m, m.latest, m.latest2) : fmtMetric(m, m.latest)) : <span className="muted">no data</span>}</span>
                  <span className="muted small">{m.days ? <>{fmtDate(m.last, false)}{m.change !== undefined && <span className={`chg ${m.change > 0 ? (m.higher_is_better === false ? 'bad' : 'ok') : m.change < 0 ? (m.higher_is_better === false ? 'ok' : m.higher_is_better === null ? '' : 'bad') : ''}`}> · {m.change > 0 ? '+' : ''}{m.change}% vs last week</span>}</> : m.manual ? 'log by hand' : 'from your phone'}</span>
                </span>
                {m.days > 1 && <Sparkline values={m.spark} color={m.color} width={64} height={24} goal={m.goal?.target} />}
              </Link>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { SEVERITY, WEEKDAYS, type MedLog, type Medication, type Symptom } from '../lib/types'
import { fmtDate, fmtDateTime, fmtISOTime, todayISO } from '../lib/format'
import { Confirm, Empty, Loading, PageHeader, Tabs, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { MedicationModal, SymptomModal } from '../components/forms'
import { openCreate, useOnChange } from '../components/Layout'

export default function Medications() {
  const [sp, setSp] = useSearchParams()
  const tab = (sp.get('tab') || 'today') as 'today' | 'all' | 'log' | 'symptoms'
  const { toast } = useToast()
  const [day, setDay] = useState<{ date: string; medications: Medication[] } | null>(null)
  const [all, setAll] = useState<Medication[] | null>(null)
  const [log, setLog] = useState<MedLog[] | null>(null)
  const [symptoms, setSymptoms] = useState<Symptom[] | null>(null)
  const [edit, setEdit] = useState<Medication | null>(null)
  const [editSym, setEditSym] = useState<Symptom | null>(null)
  const [del, setDel] = useState<Medication | null>(null)
  const load = () => {
    api.get<{ date: string; medications: Medication[] }>(`${V1}/medications/day/${todayISO()}`).then(setDay).catch(() => {})
    api.get<Medication[]>(`${V1}/medications?all=1`).then(setAll).catch(() => {})
    api.get<MedLog[]>(`${V1}/medications/log?from=${addDaysISO(-30)}`).then(setLog).catch(() => {})
    api.get<Symptom[]>(`${V1}/symptoms?limit=200`).then(setSymptoms).catch(() => {})
  }
  useEffect(() => { load() }, [])
  useOnChange(load)
  if (!day || !all || !log || !symptoms) return <Loading />
  const dose = async (medId: number, slot: string, status: 'taken' | 'skipped') => { try { await api.post(`${V1}/medications/${medId}/log`, { date: todayISO(), slot, status }); load() } catch (e) { toast((e as Error).message, 'err') } }
  const extra = async (medId: number) => { try { await api.post(`${V1}/medications/${medId}/log`, { date: todayISO(), slot: '', status: 'taken' }); toast('Extra dose logged'); load() } catch (e) { toast((e as Error).message, 'err') } }
  const undo = async (logId: number) => { try { await api.del(`${V1}/medications/log/${logId}`); load() } catch (e) { toast((e as Error).message, 'err') } }
  const remove = async (m: Medication) => { try { await api.del(`${V1}/medications/${m.id}`); setDel(null); load() } catch (e) { toast((e as Error).message, 'err') } }
  const removeSym = async (x: Symptom) => { try { await api.del(`${V1}/symptoms/${x.id}`); load() } catch (e) { toast((e as Error).message, 'err') } }
  const due = day.medications.reduce((a, m) => a + (m.slots || []).filter(s => s.time && !s.status).length, 0)
  return (
    <div className="stack loose">
      <PageHeader title="Medications & symptoms" sub={due ? `${due} dose${due === 1 ? '' : 's'} still due today` : all.filter(m => m.active).length ? 'Everything taken today' : 'Nothing scheduled'}
        actions={<><button type="button" className="btn" onClick={() => openCreate('symptom')}><Icon name="alert" size={15} />Log a symptom</button><button type="button" className="btn primary" onClick={() => openCreate('medication')}><Icon name="plus" size={15} />Add medication</button></>} />
      <Tabs tabs={[{ id: 'today', label: 'Today', icon: 'check-square' }, { id: 'all', label: 'All medications', icon: 'pill' }, { id: 'log', label: 'History', icon: 'list' }, { id: 'symptoms', label: 'Symptoms', icon: 'alert' }]} value={tab} onChange={t => setSp({ tab: t })} />

      {tab === 'today' && (day.medications.length === 0 ? <Empty title="Nothing scheduled today" icon="pill">{all.length ? 'No doses fall on today.' : 'Add a medication or supplement to get reminders and an adherence record.'}</Empty> : (
        <section className="card"><div className="card-body flush"><ul className="list">
          {day.medications.map(m => (m.slots || []).map(s => (
            <li key={`${m.id}-${s.time}-${s.log_id}`} className="list-row">
              <span className="list-ico" style={{ background: m.color || 'var(--accent-soft)', color: m.color ? '#fff' : undefined }}><Icon name="pill" size={15} /></span>
              <span className="grow"><b>{m.name}</b> <span className="muted small">{m.dose}</span><span className="muted small block">{s.time || 'extra dose'}{s.status ? ` · ${s.status}${s.taken_at ? ' at ' + fmtISOTime(s.taken_at) : ''}` : ' · due'}{m.adherence >= 0 ? ` · ${m.adherence}% on time, last 30 days` : ''}</span></span>
              {s.status ? <button type="button" className="btn ghost sm" onClick={() => undo(s.log_id)}>Undo</button>
                : <span className="row"><button type="button" className="btn sm primary" onClick={() => dose(m.id, s.time, 'taken')}><Icon name="check" size={14} />Taken</button><button type="button" className="btn ghost sm" onClick={() => dose(m.id, s.time, 'skipped')}>Skip</button></span>}
            </li>
          )))}
        </ul></div></section>
      ))}

      {tab === 'all' && (all.length === 0 ? <Empty title="No medications" icon="pill" /> : (
        <section className="card"><div className="card-body flush"><ul className="list">
          {all.map(m => (
            <li key={m.id} className={`list-row ${m.active ? '' : 'muted'}`}>
              <span className="list-ico" style={{ background: m.color || 'var(--accent-soft)', color: m.color ? '#fff' : undefined }}><Icon name="pill" size={15} /></span>
              <span className="grow"><b>{m.name}</b> <span className="muted small">{m.dose}{m.form ? ` · ${m.form}` : ''}</span>
                <span className="muted small block">{m.times.split(',').join(', ')} · {m.days === '1234567' ? 'every day' : WEEKDAYS.filter(([d]) => m.days.includes(String(d))).map(([, l]) => l).join(' ')}{m.stock >= 0 ? ` · ${m.stock} left` : ''}{!m.remind ? ' · no reminders' : ''}{!m.active ? ' · paused' : ''}{m.end_date ? ` · until ${fmtDate(m.end_date)}` : ''}</span>
                {m.notes && <span className="muted small block">{m.notes}</span>}</span>
              <span className="row"><button type="button" className="btn ghost sm" onClick={() => extra(m.id)} title="Log a dose outside the schedule">+ dose</button><button type="button" className="btn ghost sm" onClick={() => setEdit(m)}>Edit</button><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(m)}>Remove</button></span>
            </li>
          ))}
        </ul></div></section>
      ))}

      {tab === 'log' && (log.length === 0 ? <Empty title="No doses logged in the last 30 days" icon="list" /> : (
        <section className="card"><div className="card-body flush"><table className="table compact">
          <thead><tr><th>When</th><th>Medication</th><th>Scheduled</th><th>Status</th><th /></tr></thead>
          <tbody>{[...log].reverse().map(l => <tr key={l.id}><td>{l.taken_at ? fmtDateTime(l.taken_at) : fmtDate(l.date)}</td><td><b>{l.name}</b> <span className="muted small">{l.dose}</span></td><td className="muted">{l.slot || 'extra'}</td><td><span className={`badge ${l.status === 'taken' ? 'ok' : 'warn'}`}>{l.status}</span></td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => undo(l.id)}>Remove</button></td></tr>)}</tbody>
        </table></div></section>
      ))}

      {tab === 'symptoms' && (symptoms.length === 0 ? <Empty title="No symptoms logged" icon="smile">Log how you feel; it shows up in your journal and next to your numbers.</Empty> : (
        <section className="card"><div className="card-body flush"><ul className="list">
          {symptoms.map(x => (
            <li key={x.id} className="list-row">
              <span className={`sev sev-${x.severity}`} title={SEVERITY[x.severity]}>{x.severity}</span>
              <span className="grow"><b>{x.name}</b> <span className="muted small">{SEVERITY[x.severity]}{x.duration_min ? ` · ${x.duration_min} min` : ''}</span><span className="muted small block">{fmtDateTime(x.at)}{x.note ? ` · ${x.note}` : ''}{x.tags ? ` · ${x.tags}` : ''}</span></span>
              <span className="row"><button type="button" className="btn ghost sm" onClick={() => setEditSym(x)}>Edit</button><button type="button" className="btn ghost sm danger-text" onClick={() => removeSym(x)}>Delete</button></span>
            </li>
          ))}
        </ul></div></section>
      ))}

      {edit && <MedicationModal med={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); load() }} />}
      {editSym && <SymptomModal symptom={editSym} onClose={() => setEditSym(null)} onSaved={() => { setEditSym(null); load() }} />}
      {del && <Confirm title={`Remove ${del.name}?`} message="Its dose history goes with it." confirmLabel="Remove" onCancel={() => setDel(null)} onConfirm={() => remove(del)} />}
    </div>
  )
}

function addDaysISO(n: number): string { const d = new Date(); d.setDate(d.getDate() + n); return d.toISOString().slice(0, 10) }

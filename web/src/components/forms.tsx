import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { todayISO } from '../lib/format'
import { FORMS, WEEKDAYS, type AIStatus, type Goal, type Habit, type HabitRule, type Medication, type Metric, type Sample, type SleepSession, type Symptom, type Workout } from '../lib/types'
import { display, toCanonical, unitOf } from '../lib/units'
import { CoreHint, Field, Modal, Segmented, Switch, useToast } from './ui'
import { Select } from './Select'
import { Icon, KIND_ICON } from './icons'
import { useCatalog } from './Layout'

function nowLocal(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}
function toLocalInput(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso.slice(0, 16)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}
/** "YYYY-MM-DDTHH:MM" (wall) → ISO with the browser's offset. */
export function fromLocalInput(s: string): string {
  if (!s) return ''
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  const off = -d.getTimezoneOffset()
  const sign = off >= 0 ? '+' : '-'
  const p = (n: number) => String(Math.abs(n)).padStart(2, '0')
  return `${s.length === 16 ? s + ':00' : s}${sign}${p(Math.floor(off / 60))}:${p(off % 60)}`
}

// ---------- log a measurement ----------

export function LogModal({ metric: initial, sample, onClose, onSaved }: { metric?: string; sample?: Sample; onClose: () => void; onSaved: (s: Sample) => void }) {
  const catalog = useCatalog()
  const { toast } = useToast()
  const manual = (catalog?.metrics || []).filter(m => m.manual)
  const [key, setKey] = useState(sample?.metric || initial || 'weight')
  const m: Metric | undefined = (catalog?.metrics || []).find(x => x.key === key)
  const [v, setV] = useState(() => sample && m ? String(display(m, sample.value)[0].toFixed(m.decimals)) : '')
  const [v2, setV2] = useState(sample?.value2 !== undefined ? String(sample.value2) : '')
  const [at, setAt] = useState(sample ? toLocalInput(sample.start_at) : nowLocal())
  const [note, setNote] = useState(sample?.note || '')
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!sample) { setV(''); setV2('') } }, [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!m) return
    const num = Number(v.replace(',', '.'))
    if (!Number.isFinite(num)) { toast('Type a number', 'err'); return }
    setBusy(true)
    try {
      const body: Record<string, unknown> = { metric: key, value: toCanonical(m, num), at: fromLocalInput(at), note }
      if (m.pair) { const d = Number(v2.replace(',', '.')); if (!Number.isFinite(d)) { toast('Diastolic too', 'err'); setBusy(false); return } body.value2 = d }
      const s = sample ? await api.put<Sample>(`${V1}/samples/${sample.id}`, body) : await api.post<Sample>(`${V1}/samples`, body)
      toast(sample ? 'Saved' : `${m.name} logged`)
      onSaved(s)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const scale = m && m.unit === '1–5'
  return (
    <Modal title={sample ? `Edit ${m?.name.toLowerCase() || 'entry'}` : 'Log a measurement'} onClose={onClose} size="sm">
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        {!sample && <Field label="What">
          <Select value={key} onChange={setKey} options={manual.map(x => ({ value: x.key, label: x.name, group: x.category[0].toUpperCase() + x.category.slice(1), hint: unitOf(x) }))} />
        </Field>}
        {m && (scale ? (
          <Field group label={m.name}><Segmented value={v} onChange={setV} options={['1', '2', '3', '4', '5'].map(s => ({ value: s, label: s }))} /></Field>
        ) : m.pair ? (
          <div className="two-col tight">
            <Field label="Systolic" help="mmHg"><input inputMode="decimal" value={v} onChange={e => setV(e.target.value)} autoFocus placeholder="120" /></Field>
            <Field label="Diastolic" help="mmHg"><input inputMode="decimal" value={v2} onChange={e => setV2(e.target.value)} placeholder="80" /></Field>
          </div>
        ) : (
          <Field label={m.name} help={unitOf(m)}><input inputMode="decimal" value={v} onChange={e => setV(e.target.value)} autoFocus placeholder={m.key === 'weight' ? '80.4' : ''} /></Field>
        ))}
        <Field label="When"><input type="datetime-local" value={at} onChange={e => setAt(e.target.value)} max={nowLocal()} /></Field>
        <Field label="Note"><input value={note} onChange={e => setNote(e.target.value)} placeholder="optional" /></Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !m}>{sample ? 'Save' : 'Log'}</button></div>
      </form>
    </Modal>
  )
}

// ---------- a workout ----------

export function WorkoutModal({ workout, onClose, onSaved }: { workout?: Workout; onClose: () => void; onSaved: (w: Workout) => void }) {
  const catalog = useCatalog()
  const { toast } = useToast()
  const { settings } = useApp()
  const imp = settings?.units === 'imperial'
  const [f, setF] = useState({
    kind: workout?.kind || 'running', name: workout?.name || '', start_at: workout ? toLocalInput(workout.start_at) : nowLocal().slice(0, 11) + '07:00',
    duration_min: workout ? String(Math.round(workout.duration_s / 60)) : '30', distance: workout?.distance_m ? String((imp ? workout.distance_m / 1609.344 : workout.distance_m / 1000).toFixed(2)) : '',
    energy_kcal: workout?.energy_kcal ? String(Math.round(workout.energy_kcal)) : '', avg_hr: workout?.avg_hr ? String(Math.round(workout.avg_hr)) : '', max_hr: workout?.max_hr ? String(Math.round(workout.max_hr)) : '',
    elevation_m: workout?.elevation_m ? String(Math.round(workout.elevation_m)) : '', notes: workout?.notes || '',
  })
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))
  const num = (s: string) => { const n = Number(s.replace(',', '.')); return Number.isFinite(n) ? n : 0 }
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body = { kind: f.kind, name: f.name, start_at: fromLocalInput(f.start_at), duration_min: num(f.duration_min), distance_m: num(f.distance) * (imp ? 1609.344 : 1000), energy_kcal: num(f.energy_kcal), avg_hr: num(f.avg_hr), max_hr: num(f.max_hr), elevation_m: num(f.elevation_m), notes: f.notes }
      const w = workout ? await api.put<Workout>(`${V1}/workouts/${workout.id}`, body) : await api.post<Workout>(`${V1}/workouts`, body)
      toast(workout ? 'Saved' : 'Workout added')
      onSaved(w)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const kinds = catalog?.workout_kinds || []
  return (
    <Modal title={workout ? 'Edit workout' : 'Add a workout'} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Kind"><Select value={f.kind} onChange={v => set('kind', v)} options={kinds.map(k => ({ value: k.key, label: k.name }))} /></Field>
          <Field label="Name (optional)"><input value={f.name} onChange={e => set('name', e.target.value)} placeholder="Morning run" /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Start"><input type="datetime-local" value={f.start_at} onChange={e => set('start_at', e.target.value)} required /></Field>
          <Field label="Duration" help="minutes"><input inputMode="numeric" value={f.duration_min} onChange={e => set('duration_min', e.target.value)} required /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Distance" help={imp ? 'mi' : 'km'}><input inputMode="decimal" value={f.distance} onChange={e => set('distance', e.target.value)} /></Field>
          <Field label="Energy" help="kcal"><input inputMode="numeric" value={f.energy_kcal} onChange={e => set('energy_kcal', e.target.value)} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Average heart rate" help="bpm"><input inputMode="numeric" value={f.avg_hr} onChange={e => set('avg_hr', e.target.value)} /></Field>
          <Field label="Max heart rate" help="bpm"><input inputMode="numeric" value={f.max_hr} onChange={e => set('max_hr', e.target.value)} /></Field>
        </div>
        <Field label="Elevation gain" help="m"><input inputMode="numeric" value={f.elevation_m} onChange={e => set('elevation_m', e.target.value)} /></Field>
        <Field label="Notes"><textarea rows={2} value={f.notes} onChange={e => set('notes', e.target.value)} placeholder="How it felt, the route…" /></Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}><Icon name={KIND_ICON[f.kind] || 'activity'} size={15} />{workout ? 'Save' : 'Add'}</button></div>
      </form>
    </Modal>
  )
}

// ---------- a night ----------

export function SleepModal({ onClose, onSaved }: { onClose: () => void; onSaved: (s: SleepSession) => void }) {
  const { toast } = useToast()
  const y = new Date(); y.setDate(y.getDate() - 1)
  const p = (n: number) => String(n).padStart(2, '0')
  const yd = `${y.getFullYear()}-${p(y.getMonth() + 1)}-${p(y.getDate())}`
  const [f, setF] = useState({ start_at: `${yd}T23:00`, end_at: `${todayISO()}T07:00`, awake_min: '', deep_min: '', rem_min: '', note: '' })
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))
  const num = (s: string) => Number(s.replace(',', '.')) || 0
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const s = await api.post<SleepSession>(`${V1}/sleep`, { start_at: fromLocalInput(f.start_at), end_at: fromLocalInput(f.end_at), awake_min: num(f.awake_min), deep_min: num(f.deep_min), rem_min: num(f.rem_min), note: f.note })
      toast('Night added'); onSaved(s)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Add a night of sleep" onClose={onClose} size="sm">
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Fell asleep"><input type="datetime-local" value={f.start_at} onChange={e => set('start_at', e.target.value)} required /></Field>
          <Field label="Woke up"><input type="datetime-local" value={f.end_at} onChange={e => set('end_at', e.target.value)} required /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Awake in between" help="minutes"><input inputMode="numeric" value={f.awake_min} onChange={e => set('awake_min', e.target.value)} placeholder="0" /></Field>
          <Field label="Deep sleep" help="minutes, if known"><input inputMode="numeric" value={f.deep_min} onChange={e => set('deep_min', e.target.value)} /></Field>
        </div>
        <Field label="REM sleep" help="minutes, if known"><input inputMode="numeric" value={f.rem_min} onChange={e => set('rem_min', e.target.value)} /></Field>
        <Field label="Note"><input value={f.note} onChange={e => set('note', e.target.value)} placeholder="optional" /></Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Add</button></div>
      </form>
    </Modal>
  )
}

// ---------- a medication ----------

export function MedicationModal({ med, onClose, onSaved }: { med?: Medication; onClose: () => void; onSaved: (m: Medication) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({
    name: med?.name || '', dose: med?.dose || '', form: med?.form || 'tablet', times: med?.times || '08:00', days: med?.days || '1234567', notes: med?.notes || '',
    remind: med ? med.remind : true, stock: med && med.stock >= 0 ? String(med.stock) : '', start_date: med?.start_date || '', end_date: med?.end_date || '', active: med ? med.active : true, color: med?.color || '',
  })
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))
  const toggleDay = (d: number) => set('days', f.days.includes(String(d)) ? f.days.replace(String(d), '') : f.days + d)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body = { ...f, stock: f.stock === '' ? -1 : Number(f.stock) }
      const m = med ? await api.put<Medication>(`${V1}/medications/${med.id}`, body) : await api.post<Medication>(`${V1}/medications`, body)
      toast(med ? 'Saved' : 'Medication added'); onSaved(m)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const COLORS = ['', '#ef4444', '#f59e0b', '#22c55e', '#0ea5e9', '#6366f1', '#ec4899', '#8b5cf6']
  return (
    <Modal title={med ? 'Edit medication' : 'Add a medication'} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Name"><input value={f.name} onChange={e => set('name', e.target.value)} required autoFocus placeholder="Vitamin D3" /></Field>
          <Field label="Dose"><input value={f.dose} onChange={e => set('dose', e.target.value)} placeholder="2000 IU · 1 tablet" /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Form"><Select value={f.form} onChange={v => set('form', v)} options={FORMS.map(x => ({ value: x, label: x[0].toUpperCase() + x.slice(1) }))} /></Field>
          <Field label="Times" help="Comma separated, 24h"><input value={f.times} onChange={e => set('times', e.target.value)} placeholder="08:00, 20:00" /></Field>
        </div>
        <Field group label="Days"><div className="row wrap" style={{ gap: 4 }}>{WEEKDAYS.map(([d, l]) => <button key={d} type="button" className={`chip ${f.days.includes(String(d)) ? 'active' : ''}`} onClick={() => toggleDay(d)}>{l}</button>)}</div></Field>
        <div className="two-col tight">
          <Field label="From" help="optional"><input type="date" value={f.start_date} onChange={e => set('start_date', e.target.value)} /></Field>
          <Field label="Until" help="optional"><input type="date" value={f.end_date} onChange={e => set('end_date', e.target.value)} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Doses in stock" help="Counts down; empty = not tracked"><input inputMode="numeric" value={f.stock} onChange={e => set('stock', e.target.value)} /></Field>
          <Field group label="Colour"><div className="row wrap" style={{ gap: 6 }}>{COLORS.map(c => <button key={c || 'none'} type="button" className={`swatch ${f.color === c ? 'active' : ''}`} style={{ background: c || 'var(--border)' }} aria-label={c || 'none'} onClick={() => set('color', c)} />)}</div></Field>
        </div>
        <Field label="Notes"><input value={f.notes} onChange={e => set('notes', e.target.value)} placeholder="With food, prescribed by…" /></Field>
        <label className="check-row"><Switch checked={f.remind} onChange={v => set('remind', v)} label="Remind me" /><span>Remind me when a dose is due <span className="muted">(push, needs notifications set up)</span></span></label>
        {med && <label className="check-row"><Switch checked={f.active} onChange={v => set('active', v)} label="Active" /><span>Active</span></label>}
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{med ? 'Save' : 'Add'}</button></div>
      </form>
    </Modal>
  )
}

// ---------- a symptom ----------

export function SymptomModal({ symptom, onClose, onSaved }: { symptom?: Symptom; onClose: () => void; onSaved: (s: Symptom) => void }) {
  const { toast } = useToast()
  const [names, setNames] = useState<string[]>([])
  useEffect(() => { api.get<string[]>(`${V1}/symptoms/names`).then(setNames).catch(() => {}) }, [])
  const [f, setF] = useState({ name: symptom?.name || '', severity: symptom?.severity || 2, at: symptom ? toLocalInput(symptom.at) : nowLocal(), duration_min: symptom?.duration_min ? String(symptom.duration_min) : '', note: symptom?.note || '', tags: symptom?.tags || '' })
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body = { ...f, at: fromLocalInput(f.at), duration_min: Number(f.duration_min) || 0 }
      const s = symptom ? await api.put<Symptom>(`${V1}/symptoms/${symptom.id}`, body) : await api.post<Symptom>(`${V1}/symptoms`, body)
      toast(symptom ? 'Saved' : 'Symptom logged'); onSaved(s)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={symptom ? 'Edit symptom' : 'Log a symptom'} onClose={onClose} size="sm">
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <Field label="What"><input value={f.name} onChange={e => set('name', e.target.value)} required autoFocus list="symptom-names" placeholder="Headache" /><datalist id="symptom-names">{names.map(n => <option key={n} value={n} />)}</datalist></Field>
        <Field group label="How bad"><Segmented value={f.severity} onChange={v => set('severity', v)} options={[{ value: 1, label: 'Mild' }, { value: 2, label: 'Noticeable' }, { value: 3, label: 'Moderate' }, { value: 4, label: 'Bad' }, { value: 5, label: 'Severe' }]} size="sm" /></Field>
        <div className="two-col tight">
          <Field label="When"><input type="datetime-local" value={f.at} onChange={e => set('at', e.target.value)} /></Field>
          <Field label="Lasted" help="minutes, optional"><input inputMode="numeric" value={f.duration_min} onChange={e => set('duration_min', e.target.value)} /></Field>
        </div>
        <Field label="Note"><input value={f.note} onChange={e => set('note', e.target.value)} placeholder="What might have caused it" /></Field>
        <Field label="Tags" help="Comma separated."><input value={f.tags} onChange={e => set('tags', e.target.value)} /></Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{symptom ? 'Save' : 'Log'}</button></div>
      </form>
    </Modal>
  )
}

// ---------- a goal ----------

export function GoalModal({ metric, goal, onClose, onSaved }: { metric: Metric; goal?: Goal | null; onClose: () => void; onSaved: (g: Goal) => void }) {
  const { toast } = useToast()
  const [target, setTarget] = useState(goal ? String(display(metric, goal.target)[0]) : '')
  const [direction, setDirection] = useState(goal?.direction || metric.goal_dir || 'at_least')
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    const n = Number(target.replace(',', '.'))
    if (!Number.isFinite(n) || n <= 0) { toast('Type a target above zero', 'err'); return }
    setBusy(true)
    try { const g = await api.put<Goal>(`${V1}/goals/${metric.key}`, { target: toCanonical(metric, n), direction, period: 'day' }); toast('Goal saved'); onSaved(g) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const sleep = metric.key.startsWith('sleep_')
  return (
    <Modal title={`${goal ? 'Edit' : 'Set a'} goal: ${metric.name}`} onClose={onClose} size="sm">
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <Field group label="Every day"><Segmented value={direction} onChange={v => setDirection(v as 'at_least' | 'at_most')} options={[{ value: 'at_least', label: 'At least' }, { value: 'at_most', label: 'At most' }]} size="sm" /></Field>
        <Field label="Target" help={sleep ? 'minutes (420 = 7 h)' : unitOf(metric)}><input inputMode="decimal" value={target} onChange={e => setTarget(e.target.value)} autoFocus /></Field>
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
    </Modal>
  )
}

// ---------- a habit rule ----------

export function RuleModal({ rule, onClose, onSaved }: { rule?: HabitRule; onClose: () => void; onSaved: (r: HabitRule) => void }) {
  const { toast } = useToast()
  const { settings } = useApp()
  const catalog = useCatalog()
  const [habits, setHabits] = useState<{ connected: boolean; detail?: string; habits: Habit[] } | null>(null)
  useEffect(() => { api.get<{ connected: boolean; detail?: string; habits: Habit[] }>(`${V1}/habits/list`).then(setHabits).catch(() => setHabits({ connected: false, habits: [] })) }, [])
  const [f, setF] = useState({ trigger: rule?.trigger || 'workout', match: rule?.match || 'any', habit_id: rule ? String(rule.habit_id) : '', habit_name: rule?.habit_name || '' })
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF(x => ({ ...x, [k]: v }))
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const h = habits?.habits.find(x => String(x.id) === f.habit_id)
      const body = { trigger: f.trigger, match: f.match, habit_id: Number(f.habit_id), habit_name: h?.name || f.habit_name, enabled: true }
      const r = rule ? await api.put<HabitRule>(`${V1}/rules/${rule.id}`, body) : await api.post<HabitRule>(`${V1}/rules`, body)
      toast('Rule saved'); onSaved(r)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const goalMetrics = (catalog?.metrics || []).filter(m => m.goal_dir)
  return (
    <Modal title={rule ? 'Edit rule' : 'Tick a habit automatically'} onClose={onClose} size="sm">
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <Field group label="When"><Segmented value={f.trigger} onChange={v => { set('trigger', v as 'workout' | 'goal'); set('match', v === 'goal' ? 'steps' : 'any') }} options={[{ value: 'workout', label: 'A workout is recorded' }, { value: 'goal', label: 'A daily goal is met' }]} size="sm" /></Field>
        {f.trigger === 'workout'
          ? <Field label="Which kind"><Select value={f.match} onChange={v => set('match', v)} options={[{ value: 'any', label: 'Any workout' }, ...(catalog?.workout_kinds || []).map(k => ({ value: k.key, label: k.name }))]} /></Field>
          : <Field label="Which goal"><Select value={f.match} onChange={v => set('match', v)} options={goalMetrics.map(m => ({ value: m.key, label: m.name }))} /></Field>}
        <Field label="Tick this habit in Pocket Habits" help={habits && !habits.connected ? `Pocket Habits is not connected${habits.detail ? `: ${habits.detail}` : ''}.` : undefined}>
          {habits?.connected && habits.habits.length > 0
            ? <Select value={f.habit_id} onChange={v => set('habit_id', v)} options={habits.habits.map(h => ({ value: String(h.id), label: h.name }))} placeholder="Pick a habit" />
            : <input value={f.habit_id} onChange={e => set('habit_id', e.target.value)} placeholder="habit id" inputMode="numeric" />}
        </Field>
        {habits && !habits.connected && <p className="muted small"><Link to="/settings/apps">Connect Pocket Habits</Link>{!settings?.core_configured && <CoreHint />}</p>}
        <div className="form-actions"><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !f.habit_id}>Save</button></div>
      </form>
    </Modal>
  )
}

// ---------- AI status ----------

export function useAIStatus() {
  const [st, setSt] = useState<AIStatus | null>(null)
  useEffect(() => { api.get<AIStatus>(`${V1}/ai/status`).then(setSt).catch(() => {}) }, [])
  return st
}

export function AIStatusLine({ st }: { st: AIStatus | null }) {
  const { settings } = useApp()
  if (!st) return null
  if (st.ready) return <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{st.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} writes the recap and answers questions from your own numbers; nothing is sent anywhere else.</p>
  return (
    <div className="callout info"><Icon name="info" size={15} /><div>
      No AI connected{st.detail ? ` (${st.detail})` : ''}. <Link to="/settings/ai">Connect one</Link> for a weekly recap in words and questions about your data.
      {!settings?.core_configured && <CoreHint />}
    </div></div>
  )
}

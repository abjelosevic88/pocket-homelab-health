// What a workout's recorded route says along the way: splits, speed per point,
// and the series the charts draw. A track point is [lat, lng, altitude m,
// seconds from the start]; a heart-rate point is [seconds, bpm].
import { imperial } from './units'

export type TrackPoint = [number, number, number, number]
export type HRPoint = [number, number]

export function haversine(a: [number, number] | TrackPoint, b: [number, number] | TrackPoint): number {
  const r = 6371000, rad = Math.PI / 180
  const dLat = (b[0] - a[0]) * rad, dLng = (b[1] - a[1]) * rad
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a[0] * rad) * Math.cos(b[0] * rad) * Math.sin(dLng / 2) ** 2
  return 2 * r * Math.atan2(Math.sqrt(h), Math.sqrt(1 - h))
}

/** Metres covered up to each point. */
export function cumulative(track: TrackPoint[]): number[] {
  const out = [0]
  for (let i = 1; i < track.length; i++) out.push(out[i - 1] + haversine(track[i - 1], track[i]))
  return out
}

/** Speed (m/s) at each point over a window of about 30 s, so a single jittery fix does not show. */
export function speeds(track: TrackPoint[], dist = cumulative(track)): number[] {
  const out: number[] = []
  let lo = 0, hi = 0
  for (let i = 0; i < track.length; i++) {
    while (lo < i && track[i][3] - track[lo][3] > 15) lo++
    if (hi < i) hi = i
    while (hi < track.length - 1 && track[hi][3] - track[i][3] < 15) hi++
    const dt = track[hi][3] - track[lo][3]
    out.push(dt > 0 ? (dist[hi] - dist[lo]) / dt : 0)
  }
  return out
}

export interface Split { n: number; distance_m: number; time_s: number; pace: number; climb_m: number; avg_hr: number }

/** One split per kilometre (or mile), the last one partial when it is at least a tenth long. */
export function splits(track: TrackPoint[], hr: HRPoint[] = []): Split[] {
  if (track.length < 2 || !track.some(p => p[3] > 0)) return []
  const unit = imperial() ? 1609.344 : 1000
  const dist = cumulative(track)
  const total = dist[dist.length - 1]
  const at = (d: number): [number, number] => { // time and altitude at a distance
    let i = dist.findIndex(x => x >= d)
    if (i <= 0) return i === 0 ? [track[0][3], track[0][2]] : [track[track.length - 1][3], track[track.length - 1][2]]
    const f = (d - dist[i - 1]) / ((dist[i] - dist[i - 1]) || 1)
    return [track[i - 1][3] + f * (track[i][3] - track[i - 1][3]), track[i - 1][2] + f * (track[i][2] - track[i - 1][2])]
  }
  const out: Split[] = []
  for (let n = 1, from = 0; from < total; n++) {
    const to = Math.min(total, n * unit)
    if (to - from < unit / 10 && n > 1) break
    const [t0] = at(from), [t1] = at(to)
    let climb = 0
    for (let i = 1; i < track.length; i++) if (dist[i] > from && dist[i] <= to && track[i][2] > track[i - 1][2]) climb += track[i][2] - track[i - 1][2]
    const beats = hr.filter(h => h[0] >= t0 && h[0] <= t1)
    out.push({ n, distance_m: to - from, time_s: t1 - t0, pace: (t1 - t0) / ((to - from) / 1000), climb_m: climb, avg_hr: beats.length ? beats.reduce((a, h) => a + h[1], 0) / beats.length : 0 })
    from = to
  }
  return out
}

/** Every nth item so a chart gets at most `max` points. */
export function thin<T>(items: T[], max = 300): T[] {
  if (items.length <= max) return items
  const step = items.length / max
  const out: T[] = []
  for (let i = 0; i < max; i++) out.push(items[Math.floor(i * step)])
  out.push(items[items.length - 1])
  return out
}

/** "12:05" for seconds from the start. */
export function clock(s: number): string {
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = Math.floor(s % 60)
  return h ? `${h}:${String(m).padStart(2, '0')}:${String(r).padStart(2, '0')}` : `${m}:${String(r).padStart(2, '0')}`
}

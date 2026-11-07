// What a workout's recorded route says along the way: distance and speed per
// point for the map and the elevation chart. A track point is [lat, lng,
// altitude m, seconds from the start]. Splits and parts come from the server.

export type TrackPoint = [number, number, number, number]

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

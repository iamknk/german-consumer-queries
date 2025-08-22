import { useEffect, useMemo, useState } from "react"

// shadcn/ui
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"

// ---------------- Types from our API ----------------
type SlotAgg = {
  precision: number
  recall: number
  f1: number
  tp: number
  fp: number
  fn: number
}
type ProviderMetrics = {
  slot_precision: number
  slot_recall: number
  f1: number
  exact_match: number
  jaccard: number
  avg_latency_ms: number
  count: number
  ambiguity_handling_rate: number
  per_slot: Record<string, SlotAgg>
}
type EvalResp = { openai?: ProviderMetrics; claude?: ProviderMetrics }

type ParseResponse = {
  location: string
  dates: { checkin: string; checkout: string }
  guests: { adults: number; children: number }
  price_max_eur: number
  stars_min: number
  rating_min: number
  family_friendly: boolean
  ui_filters: {
    meals: string[]
    ratings: string[]
    hotelTypes: string[]
    hotelfacilities: string[]
    poolbeach: string[]
    distanceBeach: string[]
    travelGroup: string[]
    stars: string[]
    wellness: string[]
    reference_distance_max: string[]
    flex: string[]
    children: string[]
    parking: string[]
    freetime: string[]
    certifications: string[]
    hotelthemes: string[]
    hotelBrand: string[]
    hotelinformation: string[]
  }
  unsupported_criteria: string[]
}
type StoredEntry = {
  query: string
  latency_ms: number
  response: { openai?: ParseResponse; claude?: ParseResponse }
  time: string
}

// ---------------- Helpers ----------------
function pct(n?: number) {
  if (n == null) return "—"
  return `${Math.round(n * 100)}%`
}
function ms(n?: number) {
  if (n == null) return "—"
  return `${Math.round(n)} ms`
}
function MetricCard({
  title,
  value,
  hint,
  max = 1,
  format = "pct",
}: {
  title: string
  value?: number
  hint?: string
  max?: number
  format?: "pct" | "raw"
}) {
  const v = value ?? 0
  const percent = Math.max(0, Math.min(100, Math.round((v / max) * 100)))
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-semibold">
          {format === "raw" ? v.toFixed(2) : pct(v)}
        </div>
        {hint && <div className="text-xs text-muted-foreground mt-1">{hint}</div>}
        <Progress value={percent} className="mt-3" />
      </CardContent>
    </Card>
  )
}

// mirror of backend flatten, simplified for viewing diffs
function flatten(p: ParseResponse) {
  const s = new Set<string>()
  const push = (k: string, v: string | number | boolean | undefined | null) => {
    if (v === undefined || v === null || v === "" || v === 0) return
    s.add(`${k}=${v}`)
  }
  if (!p) return s
  push("location", p.location)
  push("dates.checkin", p.dates?.checkin)
  push("dates.checkout", p.dates?.checkout)
  push("guests.adults", p.guests?.adults)
  push("guests.children", p.guests?.children)
  push("price_max_eur", p.price_max_eur)
  push("stars_min", p.stars_min)
  push("rating_min", p.rating_min)
  push("family_friendly", p.family_friendly)

  const addSlice = (pref: string, arr?: string[]) => {
    (arr ?? []).forEach((v) => v && s.add(`${pref}=${v}`))
  }
  const ui = p.ui_filters || ({} as ParseResponse["ui_filters"])
  addSlice("ui.meals", ui.meals)
  addSlice("ui.ratings", ui.ratings)
  addSlice("ui.hotelTypes", ui.hotelTypes)
  addSlice("ui.hotelfacilities", ui.hotelfacilities)
  addSlice("ui.poolbeach", ui.poolbeach)
  addSlice("ui.distanceBeach", ui.distanceBeach)
  addSlice("ui.travelGroup", ui.travelGroup)
  addSlice("ui.stars", ui.stars)
  addSlice("ui.wellness", ui.wellness)
  addSlice("ui.reference_distance_max", ui.reference_distance_max)
  addSlice("ui.flex", ui.flex)
  addSlice("ui.children", ui.children)
  addSlice("ui.parking", ui.parking)
  addSlice("ui.freetime", ui.freetime)
  addSlice("ui.certifications", ui.certifications)
  addSlice("ui.hotelthemes", ui.hotelthemes)
  addSlice("ui.hotelBrand", ui.hotelBrand)
  addSlice("ui.hotelinformation", ui.hotelinformation)

  ;(p.unsupported_criteria ?? []).forEach((v) => v && s.add(`unsupported=${v}`))
  return s
}
function diffSets(a?: ParseResponse, b?: ParseResponse) {
  const A = a ? flatten(a) : new Set<string>()
  const B = b ? flatten(b) : new Set<string>()
  const onlyA: string[] = []
  const onlyB: string[] = []
  const both: string[] = []
  A.forEach((k) => (B.has(k) ? both.push(k) : onlyA.push(k)))
  B.forEach((k) => {
    if (!A.has(k)) onlyB.push(k)
  })
  return {
    onlyA: onlyA.sort(),
    onlyB: onlyB.sort(),
    both: both.sort(),
  }
}

type Row = { slot: string; openai?: SlotAgg; claude?: SlotAgg }
function makePerSlotRows(openai?: ProviderMetrics, claude?: ProviderMetrics): Row[] {
  const rows: Record<string, Row> = {}
  const add = (who: "openai" | "claude", m?: ProviderMetrics) => {
    if (!m?.per_slot) return
    Object.entries(m.per_slot).forEach(([slot, agg]) => {
      rows[slot] ||= { slot }
      ;(rows[slot] as any)[who] = agg
    })
  }
  add("openai", openai)
  add("claude", claude)
  return Object.values(rows).sort((a, b) => a.slot.localeCompare(b.slot))
}

// ---------------- Page ----------------
export default function EvaluationsPage() {
  const [metrics, setMetrics] = useState<EvalResp | null>(null)
  const [runs, setRuns] = useState<StoredEntry[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [sortKey, setSortKey] = useState<"slot" | "openai_f1" | "claude_f1">("slot")

  const load = async () => {
    setLoading(true)
    try {
      const [m, raw] = await Promise.all([
        fetch("http://localhost:8080/v1/evaluations").then((res) => res.json() as Promise<EvalResp>),
        fetch("http://localhost:8080/v1/evaluations?raw=1").then(async (res) => (res.ok ? ((await res.json()) as StoredEntry[]) : [])),
      ])
      setMetrics(m)
      setRuns(raw)
    } catch {
      setMetrics(null)
      setRuns(null)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const latest = useMemo(() => {
    if (!runs || runs.length === 0) return null
    return runs[runs.length - 1]
  }, [runs])

  const latestDiff = useMemo(() => {
    if (!latest) return null
    return diffSets(latest.response.openai, latest.response.claude)
  }, [latest])

  const perSlotRows = useMemo(() => {
    const rows = makePerSlotRows(metrics?.openai, metrics?.claude)
    if (sortKey === "slot") return rows
    if (sortKey === "openai_f1") {
      return [...rows].sort((a, b) => (b.openai?.f1 ?? -1) - (a.openai?.f1 ?? -1))
    }
    if (sortKey === "claude_f1") {
      return [...rows].sort((a, b) => (b.claude?.f1 ?? -1) - (a.claude?.f1 ?? -1))
    }
    return rows
  }, [metrics, sortKey])

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Evaluations</h1>
        <div className="flex items-center gap-2">
          <Badge variant="secondary">{new Date().toLocaleString()}</Badge>
          <Button onClick={load} disabled={loading}>{loading ? "Refreshing…" : "Refresh"}</Button>
        </div>
      </div>

      {!metrics && (
        <Alert>
          <AlertTitle>No evaluations yet</AlertTitle>
          <AlertDescription>
            Submit some queries from the Parser page (try **Both**) and return here.
          </AlertDescription>
        </Alert>
      )}

      {metrics && (
        <Tabs defaultValue="summary">
          <TabsList>
            <TabsTrigger value="summary">Summary</TabsTrigger>
            <TabsTrigger value="per-slot">Per‑slot</TabsTrigger>
            <TabsTrigger value="latest">Latest Diff</TabsTrigger>
            <TabsTrigger value="history">History</TabsTrigger>
          </TabsList>

          {/* Summary: metric cards for OpenAI and Claude */}
          <TabsContent value="summary" className="space-y-6 pt-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              {/* OpenAI */}
              <Card>
                <CardHeader>
                  <CardTitle>
                    OpenAI {metrics.openai ? <Badge className="ml-2" variant="outline">{metrics.openai.count} runs</Badge> : null}
                  </CardTitle>
                </CardHeader>
                <CardContent className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                  <MetricCard title="Slot Precision" value={metrics.openai?.slot_precision} hint="Micro across slots" />
                  <MetricCard title="Slot Recall" value={metrics.openai?.slot_recall} />
                  <MetricCard title="F1" value={metrics.openai?.f1} />
                  <MetricCard title="Exact Match" value={metrics.openai?.exact_match} />
                  <MetricCard title="Jaccard" value={metrics.openai?.jaccard} />
                  <MetricCard title="Avg Latency" value={(metrics.openai?.avg_latency_ms ?? 0) / 2000} hint={ms(metrics.openai?.avg_latency_ms)} max={1} />
                  <MetricCard title="Ambiguity Handling" value={metrics.openai?.ambiguity_handling_rate} hint="On ambiguous queries" />
                </CardContent>
              </Card>

              {/* Claude */}
              <Card>
                <CardHeader>
                  <CardTitle>
                    Claude {metrics.claude ? <Badge className="ml-2" variant="outline">{metrics.claude.count} runs</Badge> : null}
                  </CardTitle>
                </CardHeader>
                <CardContent className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                  <MetricCard title="Slot Precision" value={metrics.claude?.slot_precision} hint="Micro across slots" />
                  <MetricCard title="Slot Recall" value={metrics.claude?.slot_recall} />
                  <MetricCard title="F1" value={metrics.claude?.f1} />
                  <MetricCard title="Exact Match" value={metrics.claude?.exact_match} />
                  <MetricCard title="Jaccard" value={metrics.claude?.jaccard} />
                  <MetricCard title="Avg Latency" value={(metrics.claude?.avg_latency_ms ?? 0) / 2000} hint={ms(metrics.claude?.avg_latency_ms)} max={1} />
                  <MetricCard title="Ambiguity Handling" value={metrics.claude?.ambiguity_handling_rate} hint="On ambiguous queries" />
                </CardContent>
              </Card>
            </div>
          </TabsContent>

          {/* Per-slot table */}
          <TabsContent value="per-slot" className="pt-4">
            <Card>
              <CardHeader className="flex flex-row items-center justify-between">
                <CardTitle>Per‑slot precision/recall/F1</CardTitle>
                <div className="flex items-center gap-2">
                  <Button variant={sortKey === "slot" ? "default" : "outline"} onClick={() => setSortKey("slot")}>Sort: Slot</Button>
                  <Button variant={sortKey === "openai_f1" ? "default" : "outline"} onClick={() => setSortKey("openai_f1")}>Sort: OpenAI F1</Button>
                  <Button variant={sortKey === "claude_f1" ? "default" : "outline"} onClick={() => setSortKey("claude_f1")}>Sort: Claude F1</Button>
                </div>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-[28%]">Slot</TableHead>
                      <TableHead>OpenAI P/R/F1</TableHead>
                      <TableHead>OpenAI TP/FP/FN</TableHead>
                      <TableHead>Claude P/R/F1</TableHead>
                      <TableHead>Claude TP/FP/FN</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {perSlotRows.map((r) => (
                      <TableRow key={r.slot}>
                        <TableCell className="font-medium">{r.slot}</TableCell>
                        <TableCell className="text-sm">
                          {r.openai ? (
                            <>
                              {pct(r.openai.precision)} / {pct(r.openai.recall)} / {pct(r.openai.f1)}
                            </>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="text-sm">
                          {r.openai ? `${r.openai.tp}/${r.openai.fp}/${r.openai.fn}` : <span className="text-muted-foreground">—</span>}
                        </TableCell>
                        <TableCell className="text-sm">
                          {r.claude ? (
                            <>
                              {pct(r.claude.precision)} / {pct(r.claude.recall)} / {pct(r.claude.f1)}
                            </>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="text-sm">
                          {r.claude ? `${r.claude.tp}/${r.claude.fp}/${r.claude.fn}` : <span className="text-muted-foreground">—</span>}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          </TabsContent>

          {/* Latest diff */}
          <TabsContent value="latest" className="space-y-4 pt-4">
            {!latest ? (
              <Alert>
                <AlertTitle>No history yet</AlertTitle>
                <AlertDescription>Run at least one query with “Both” to compare outputs.</AlertDescription>
              </Alert>
            ) : (
              <>
                <Card>
                  <CardHeader>
                    <CardTitle>Latest Query</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="text-sm">{latest.query}</div>
                    <div className="text-xs text-muted-foreground mt-1">
                      {new Date(latest.time).toLocaleString()} • {ms(latest.latency_ms)}
                    </div>
                  </CardContent>
                </Card>

                <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
                  <Card className="lg:col-span-1">
                    <CardHeader><CardTitle>Only in OpenAI</CardTitle></CardHeader>
                    <CardContent className="space-y-2">
                      {latestDiff?.onlyA?.length ? latestDiff.onlyA.map((k) => (
                        <Badge key={k} variant="secondary" className="mr-2">{k}</Badge>
                      )) : <div className="text-sm text-muted-foreground">—</div>}
                    </CardContent>
                  </Card>
                  <Card className="lg:col-span-1">
                    <CardHeader><CardTitle>Only in Claude</CardTitle></CardHeader>
                    <CardContent className="space-y-2">
                      {latestDiff?.onlyB?.length ? latestDiff.onlyB.map((k) => (
                        <Badge key={k} variant="secondary" className="mr-2">{k}</Badge>
                      )) : <div className="text-sm text-muted-foreground">—</div>}
                    </CardContent>
                  </Card>
                  <Card className="lg:col-span-1">
                    <CardHeader><CardTitle>Overlap</CardTitle></CardHeader>
                    <CardContent className="space-y-2">
                      {latestDiff?.both?.length ? latestDiff.both.map((k) => (
                        <Badge key={k} variant="outline" className="mr-2">{k}</Badge>
                      )) : <div className="text-sm text-muted-foreground">—</div>}
                    </CardContent>
                  </Card>
                </div>

                <Separator className="my-4" />

                <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                  <Card>
                    <CardHeader><CardTitle>OpenAI JSON</CardTitle></CardHeader>
                    <CardContent>
                      <pre className="max-h-[420px] overflow-auto rounded bg-muted p-3 text-xs">
{JSON.stringify(latest.response.openai, null, 2)}
                      </pre>
                    </CardContent>
                  </Card>
                  <Card>
                    <CardHeader><CardTitle>Claude JSON</CardTitle></CardHeader>
                    <CardContent>
                      <pre className="max-h-[420px] overflow-auto rounded bg-muted p-3 text-xs">
{JSON.stringify(latest.response.claude, null, 2)}
                      </pre>
                    </CardContent>
                  </Card>
                </div>
              </>
            )}
          </TabsContent>

          {/* History table */}
          <TabsContent value="history" className="pt-4">
            {!runs || runs.length === 0 ? (
              <Alert>
                <AlertTitle>No runs yet</AlertTitle>
                <AlertDescription>Once you have queries stored, they’ll appear here.</AlertDescription>
              </Alert>
            ) : (
              <Card>
                <CardHeader><CardTitle>All Runs</CardTitle></CardHeader>
                <CardContent>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-[40%]">Query</TableHead>
                        <TableHead>Time</TableHead>
                        <TableHead>Latency</TableHead>
                        <TableHead>OpenAI</TableHead>
                        <TableHead>Claude</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {runs.map((r, i) => (
                        <TableRow key={i}>
                          <TableCell className="text-sm">{r.query}</TableCell>
                          <TableCell className="text-xs text-muted-foreground">{new Date(r.time).toLocaleString()}</TableCell>
                          <TableCell>{ms(r.latency_ms)}</TableCell>
                          <TableCell>{r.response.openai ? <Badge>✓</Badge> : <Badge variant="destructive">—</Badge>}</TableCell>
                          <TableCell>{r.response.claude ? <Badge>✓</Badge> : <Badge variant="destructive">—</Badge>}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            )}
          </TabsContent>
        </Tabs>
      )}
    </div>
  )
}

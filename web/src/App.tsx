import { useState } from 'react'
import EvaluationsPage from './pages/EvaluationsPage'

type Dates = { checkin: string; checkout: string }
type Guests = { adults: number; children: number }
type UiFilters = {
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
type ParseResponse = {
  location: string
  dates: Dates
  guests: Guests
  price_max_eur: number
  stars_min: number
  rating_min: number
  family_friendly: boolean
  ui_filters: UiFilters
  unsupported_criteria: string[]
}
type MultiParseResponse = {
  openai?: ParseResponse
  claude?: ParseResponse
}

function App() {
  const [query, setQuery] = useState(
    'Familienfreundliches Hotel mit Frühstück und WLAN unter 120€ in Berlin vom 12.–14.10.2025, 2 Erwachsene, 1 Kind.'
  )
  const [provider, setProvider] = useState<"openai" | "claude" | "both">("openai")
  const [result, setResult] = useState<MultiParseResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [showEval, setShowEval] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setLoading(true)
    setResult(null)

    try {
      const res = await fetch('http://localhost:8080/v1/parse', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query_de: query, provider }),
      })

      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || res.statusText)
      }

      const data: MultiParseResponse = await res.json()
      setResult(data)
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : 'Unbekannter Fehler bei der Anfrage.'
      setError(message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ maxWidth: 1000, margin: '0 auto', padding: 24 }}>
      <h1>Hotel Query Parser (LLM-backed)</h1>
      <div className="p-4">
      <button
        className="bg-blue-600 text-white px-4 py-2 rounded"
        onClick={() => setShowEval(!showEval)}
      >
        {showEval ? "Back to Parser" : "View Evaluations"}
      </button>
      {showEval ? <EvaluationsPage /> : <p></p>}
    </div>
      <form
        onSubmit={handleSubmit}
        style={{ display: 'flex', gap: 8, marginBottom: 16 }}
      >
        <input
          style={{ flex: 1, padding: 8, border: '1px solid #ccc' }}
          placeholder="Beschreibe, wonach du suchst…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Hotelsuchanfrage"
        />
        <select
          value={provider}
          onChange={(e) =>
            setProvider(e.target.value as 'openai' | 'claude' | 'both')
          }
          style={{ padding: 8 }}
        >
          <option value="openai">OpenAI</option>
          <option value="claude">Claude</option>
          <option value="both">Both</option>
        </select>
        <button type="submit" disabled={loading} style={{ padding: '8px 16px' }}>
          {loading ? '…' : 'Parse'}
        </button>
      </form>

      {error && (
        <div style={{ color: 'red', marginBottom: 8, whiteSpace: 'pre-wrap' }}>
          <strong>Fehler:</strong> {error}
        </div>
      )}

      {result && (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          {result.openai && (
            <div>
              <h2>OpenAI</h2>
              <pre
                style={{
                  background: '#f7f7f7',
                  padding: 12,
                  borderRadius: 6,
                  overflow: 'auto',
                  maxHeight: 480,
                }}
              >
                {JSON.stringify(result.openai, null, 2)}
              </pre>
            </div>
          )}
          {result.claude && (
            <div>
              <h2>Claude</h2>
              <pre
                style={{
                  background: '#f7f7f7',
                  padding: 12,
                  borderRadius: 6,
                  overflow: 'auto',
                  maxHeight: 480,
                }}
              >
                {JSON.stringify(result.claude, null, 2)}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default App

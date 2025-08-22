# Simple German Hotel Query Parser — Go API + React Frontend (OpenAI-powered)

This version calls **OpenAI GPT‑5** (or `gpt-5-mini`) to parse German hotel queries into your strict schema using a **system prompt + few-shots**.

## Prereqs
- Go 1.22+
- Node 18+
- OpenAI API key

## Setup

### Backend
```bash
cd api
cp .env.sample .env
# edit .env and set OPENAI_API_KEY
go run .
```

### Frontend
```bash
cd web
npm install
npm run dev
```

Backend: http://localhost:8080  
Frontend: http://localhost:5173

### Endpoint
`POST /v1/parse`
```json
{ "query_de": "Familienfreundliches Hotel mit Frühstück und WLAN unter 120€ in Berlin vom 12.–14.10.2025, 2 Erwachsene, 1 Kind." }
```

### Notes
- Uses Chat Completions with `response_format: {"type":"json_object"}` and `temperature: 0`.
- Strict JSON decoding with `DisallowUnknownFields` + range checks.
- You can override the system prompt in `api/prompt/system.txt` and add few-shots in `api/prompt/examples.json`.

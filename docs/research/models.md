# Models and providers

The question: what are the options for the models behind the **Editor**'s acts, open-weight and closed, hosted and self-hosted, and how do they compare in quality, cost and data handling for go-slow's two workloads: the daily acts (bounded extraction and classification with JSON-schema output, about 100 calls a day) and the **Sunday read** (weekly long-context reasoning with tool calls)?

## Opinion

**Cost doesn't choose the model; quality and data handling do.** At go-slow's volume the daily acts cost about $2–6 a month on any cheap candidate, open or closed, and a **Sunday read** between about $0.10 and $1.60 a run. The design's consequences are in [`../system-design/README.md`](../system-design/README.md) (*The model is an adapter*, *A provider never trains on go-slow's requests*).

- **The daily acts are where open-weight models are competitive.** On schema-valid extraction the best benchmark found puts open and closed within about a point (its host has a conflict of interest).
- **The Sunday read is where they are furthest apart.** On Artificial Analysis's index (October 2026) the best closed model scores 58 and the best open 46; Epoch estimates open models trail by about four months. Open models trail most on hallucination and knowledge, which a context-setting analysis leans on. Kimi K3 leads the independent long-context evaluation among open models.
- **Self-hosting is not the starting point.** A 64–128 GB Apple silicon Mac runs the 27–35B class and gpt-oss-120b, plausible for the daily acts, but none of the frontier open models (320B and up). The **Owner**'s machine is not suited to it; the adapter keeps it open for later.
- **Providers differ most on data handling.** Fireworks retains nothing by default for serverless inference, but its JSON-schema mode turns reasoning off. OpenRouter retains nothing unless opted in and can be restricted to zero-retention endpoints per request, behind one API for open and closed models (its pricing was not researched). Together stores requests until zero retention is switched on. DeepSeek's own API stores data in China and lists training on inputs, so it is excluded.
- **Licences** of the leading open models are MIT or MIT-like, restricting only model-as-a-service businesses at scale. Meta's Muse Spark Open is the exception (branding, usage policy, gated download).

**Shortlist for the first recorded runs**, two open-weight and two closed from different vendors per workload, so measurement decides:

| Workload | Open-weight | Closed |
|---|---|---|
| Daily acts | GLM-5.3-Flash, DeepSeek V4.1 Flash (via Fireworks or OpenRouter, zero retention) | Claude Haiku 5.5, GPT-6 Luna |
| Sunday read | Kimi K3, GLM-5.3 | Claude Opus 5.5, GPT-6.1 Sol |

**To check before using a closed model:** its vendor's API retention and training terms. The research did not cover them; that they don't train on API requests and keep them for a limited period is recalled, not sourced.

## Grounding

One research pass, briefed from the question and asked to be even-handed between vendors. Sources: model cards on Hugging Face, providers' pricing and data-handling pages (Fireworks, Together, OpenRouter, DeepSeek, DeepInfra, Groq), serving software documentation (vLLM, llama.cpp, Ollama, LM Studio, mlx-lm), Artificial Analysis and Epoch AI, and closed vendors' pricing pages.

Evidence quality:
- **Read through a summarising fetch, never verbatim:** every page. JavaScript-rendered leaderboards came through only partly.
- **Low quality:** Mac throughput figures beyond llama.cpp's own table are user reports.
- **Not checked:** licences of Step 5 and the large Qwen; DeepInfra's, Groq's, Z.ai's and Moonshot's retention; closed vendors' retention.
- **Own arithmetic:** the cost estimates, from sourced prices at about 12M input and 1.5M output tokens a month for the daily acts.
- **Where sources corrected recall:** which model families lead; the Qwen and Gemma licences; Together's default retention; which tool-calling benchmark is current.

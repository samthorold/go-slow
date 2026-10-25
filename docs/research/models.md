# Models and providers

The question: what are the options for the models behind the **Editor**'s acts, open-weight and closed, hosted and self-hosted, and how do they compare in quality, cost and data handling for go-slow's two workloads: the daily acts (bounded extraction and classification with JSON-schema output, about 100 calls a day) and the **Sunday read** (weekly long-context reasoning with tool calls)?

## Opinion

**Cost doesn't choose the model; quality and data handling do.** At go-slow's volume the daily acts cost about $2–6 a month on any cheap candidate, open or closed, and a **Sunday read** between about $0.10 and $1.60 a run. The design's consequences are in [`../system-design/README.md`](../system-design/README.md) (*The model is an adapter*, *A provider never trains on go-slow's requests*).

- **The daily acts are where open-weight models are competitive.** On schema-valid extraction the best benchmark found puts open and closed within about a point (its host has a conflict of interest).
- **The Sunday read is where they are furthest apart.** On Artificial Analysis's index (October 2026) the best closed model scores 58 and the best open 46; Epoch estimates open models trail by about four months. Open models trail most on hallucination and knowledge, which a context-setting analysis leans on. Kimi K3 leads the independent long-context evaluation among open models.
- **Self-hosting is not the starting point.** A 64–128 GB Apple silicon Mac runs the 27–35B class and gpt-oss-120b, plausible for the daily acts, but none of the frontier open models (320B and up). The **Owner**'s machine is not suited to it; the adapter keeps it open for later.
- **Providers differ most on data handling.** Fireworks retains nothing by default for serverless inference, but its JSON-schema mode turns reasoning off. OpenRouter retains nothing unless opted in and can be restricted to zero-retention endpoints per request, behind one API for open and closed models (its pricing was not researched). Together stores requests until zero retention is switched on. DeepSeek's own API stores data in China and lists training on inputs, so it is excluded.
- **Licences** of the leading open models are MIT or MIT-like, restricting only model-as-a-service businesses at scale. Meta's Muse Spark Open is the exception (branding, usage policy, gated download).

**Provider: Claude only.** Open-weight models from an inference provider are paid per token like Claude, and at go-slow's volume the difference is a few dollars a month at most, less than a second adapter, a second set of recordings and a measurement between them cost to build and keep. The findings above on open-weight models stay as the grounding for that call. Hosts per hour (dedicated endpoints, rented GPUs) were not researched; a model the **Owner** runs is the one way open-weight is free at the margin, and it needs a machine running all day (see *Self-hosting* above).

**Models**, chosen by recorded runs among Claude models: Claude Haiku 5.5 for the daily acts, Claude Opus 5.5 for the **Sunday read**.

**The model IDs are pinned snapshots.** Each act is configured with the most specific ID Anthropic offers, and for these models that is the dateless ID itself: `claude-haiku-5-5` (reading, placing, connecting, the Briefing) and `claude-opus-5-5` (the **Sunday read**). From the 4.6 generation on, Anthropic's dateless ID is not an alias but the snapshot: "the dateless ID is the canonical model ID for that release. It maps to a single, fixed model snapshot. Anthropic does not update the weights or configuration of an existing model ID. When an updated version is available, it ships under a new model ID" ([Model IDs and versioning](https://platform.claude.com/docs/en/about-claude/models/model-ids-and-versions)). The [models overview](https://platform.claude.com/docs/en/models/overview) lists the same string as both API ID and alias for each, and says "Dateless IDs are their own pinned snapshot; the alias row repeats them". Only models before the 4.6 generation have dated snapshots (`claude-haiku-4-5-20251001`) behind a moving alias (`claude-haiku-4-5`), so an ID with a date is the pin for those, and the dateless form is not. The Models API (`GET /v1/models`, October 2026) lists `claude-haiku-5-5` and `claude-opus-5-5` with no dated form, while it lists the 4.5 models by their dated IDs. One caveat from the same page: the serving infrastructure around a fixed ID (routing, safety classifiers, sampling) can change, and "occasionally, infrastructure updates produce minor differences in observable behavior even when the model ID and weights have not changed", which a pin can't prevent.

**To check against sources before relying on them:** Anthropic's API retention and training terms (that it doesn't train on API requests and keeps them for a limited period is recalled, not sourced).

## Grounding

One research pass, briefed from the question and asked to be even-handed between vendors. Sources: model cards on Hugging Face, providers' pricing and data-handling pages (Fireworks, Together, OpenRouter, DeepSeek, DeepInfra, Groq), serving software documentation (vLLM, llama.cpp, Ollama, LM Studio, mlx-lm), Artificial Analysis and Epoch AI, and closed vendors' pricing pages.

Evidence quality:
- **Read through a summarising fetch, never verbatim:** every page. JavaScript-rendered leaderboards came through only partly.
- **Low quality:** Mac throughput figures beyond llama.cpp's own table are user reports.
- **Not checked:** licences of Step 5 and the large Qwen; DeepInfra's, Groq's, Z.ai's and Moonshot's retention; closed vendors' retention.
- **Own arithmetic:** the cost estimates, from sourced prices at about 12M input and 1.5M output tokens a month for the daily acts.
- **Model IDs:** read in full from Anthropic's models overview and *Model IDs and versioning* pages (through a summarising fetch, with the quoted sentences asked for verbatim), and checked against the Models API's own list, which costs no model call. Recall had dateless IDs as aliases that might move; the sources say that holds only before the 4.6 generation.
- **Where sources corrected recall:** which model families lead; the Qwen and Gemma licences; Together's default retention; which tool-calling benchmark is current.

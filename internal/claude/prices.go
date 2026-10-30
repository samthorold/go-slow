package claude

import "github.com/samthorold/go-slow/internal/editor"

// PricesAsOf is the date the prices below were read, and PricesSource where
// from. Prices change and nothing here sees it: a run's dry run prints this
// date beside its estimate, so a stale table shows where it is used, and
// the table is read again before a run whose cap matters (system design:
// Every judgement carries its judge).
const (
	PricesAsOf   = "2026-10-06"
	PricesSource = "Anthropic's model pricing, https://platform.claude.com/docs/en/about-claude/pricing (first-party API rates)"
)

// prices are the first-party API's prices per million tokens, at real-time
// rates; a batch is charged half. Claude Haiku 5.5's are for requests up to
// 100K input tokens, beyond which it is charged $0.50 in and $2.50 out.
var prices = map[string]editor.Price{
	"claude-haiku-5-5": {
		InputPerMTok: 0.10, OutputPerMTok: 0.50,
		LongAbove: 100_000, LongInputPerMTok: 0.50, LongOutputPerMTok: 2.50,
	},
	"claude-opus-5-5": {InputPerMTok: 4, OutputPerMTok: 20},
}

// PriceOf is a model's price at real-time rates, reporting whether the
// table has it.
func PriceOf(model string) (editor.Price, bool) {
	p, ok := prices[model]
	return p, ok
}

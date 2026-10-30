package claude

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestEveryModelAnActIsPinnedToHasAPrice(t *testing.T) {
	f, err := os.Open("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		name, value, ok := strings.Cut(lines.Text(), "=")
		if !ok || !strings.HasSuffix(name, "_MODEL") {
			continue
		}
		if _, ok := PriceOf(value); !ok {
			t.Errorf("%s=%s has no price: a run on it can't be priced", name, value)
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
}

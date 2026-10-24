package polite

import (
	"bufio"
	"strings"
)

// agentToken is the product token go-slow matches robots.txt groups by.
const agentToken = "go-slow"

type rule struct {
	allow   bool
	pattern string
}

// robots is the rules of a robots.txt (RFC 9309) that apply to go-slow.
type robots struct {
	rules []rule
	// disallowAll is set when the robots.txt was unreachable: RFC 9309 has
	// a crawler assume complete disallow.
	disallowAll bool
}

// parseRobots reads the groups that apply to go-slow: those naming it, or
// failing any, those for "*".
func parseRobots(body string) robots {
	var own, any []rule
	var agents []string
	inRules := false
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if inRules {
				agents, inRules = nil, false
			}
			agents = append(agents, strings.ToLower(value))
		case "allow", "disallow":
			inRules = true
			if value == "" {
				continue // an empty Disallow allows everything
			}
			r := rule{allow: key == "allow", pattern: value}
			for _, a := range agents {
				switch a {
				case agentToken:
					own = append(own, r)
				case "*":
					any = append(any, r)
				}
			}
		}
	}
	for _, a := range agents {
		if a == agentToken && own == nil {
			own = []rule{} // a group naming go-slow with no rules still wins
		}
	}
	if own != nil {
		return robots{rules: own}
	}
	return robots{rules: any}
}

// allowed reports whether go-slow may fetch path: the longest matching rule
// decides, and on a tie Allow wins.
func (r robots) allowed(path string) bool {
	if r.disallowAll {
		return false
	}
	best, allow := -1, true
	for _, rl := range r.rules {
		if !match(rl.pattern, path) {
			continue
		}
		n := len(rl.pattern)
		if n > best || (n == best && rl.allow) {
			best, allow = n, rl.allow
		}
	}
	return allow
}

// match reports whether path matches a robots.txt pattern, where * matches
// any run of characters and a trailing $ anchors the end.
func match(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	rest := path[len(parts[0]):]
	for _, p := range parts[1:] {
		i := strings.Index(rest, p)
		if i < 0 {
			return false
		}
		rest = rest[i+len(p):]
	}
	if anchored && len(parts) > 1 {
		return strings.HasSuffix(path, parts[len(parts)-1])
	}
	return !anchored || rest == ""
}

package anvil

import (
	"fmt"
	"strings"
)

func parseDotenv(data string) (map[string]string, error) {
	vars := make(map[string]string)

	for n, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

		key, rest, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d: want KEY=VALUE", n+1)
		}

		value, err := dotenvValue(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", n+1, key, err)
		}

		vars[key] = value
	}

	return vars, nil
}

func dotenvValue(s string) (string, error) {
	switch {
	case strings.HasPrefix(s, "'"):
		value, rest, ok := strings.Cut(s[1:], "'")
		if !ok {
			return "", fmt.Errorf("unterminated single quote")
		}
		return value, trailing(rest)

	case strings.HasPrefix(s, `"`):
		var b strings.Builder

		for i := 1; i < len(s); i++ {
			switch c := s[i]; c {
			case '"':
				return b.String(), trailing(s[i+1:])
			case '\\':
				i++
				if i == len(s) {
					return "", fmt.Errorf("unterminated escape")
				}
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(s[i])
				}
			default:
				b.WriteByte(c)
			}
		}

		return "", fmt.Errorf("unterminated double quote")

	default:
		for _, sep := range []string{" #", "\t#"} {
			if i := strings.Index(s, sep); i >= 0 {
				s = s[:i]
			}
		}
		return strings.TrimSpace(s), nil
	}
}

func trailing(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "#") {
		return nil
	}

	return fmt.Errorf("unexpected text after the closing quote")
}

func envQuote(value string) string {
	if !strings.ContainsAny(value, " \t\n\r#\"'`$\\") {
		return value
	}

	if !strings.ContainsAny(value, "'\n\r") {
		return "'" + value + "'"
	}

	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`, "\r", `\r`)
	return `"` + r.Replace(value) + `"`
}

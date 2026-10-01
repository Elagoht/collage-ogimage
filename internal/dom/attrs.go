package dom

// attribute is one attribute of a raw start tag, with where its name and its
// value begin in the tag.
type attribute struct {
	name, value     string
	nameAt, valueAt int
}

// scanAttributes reads the attributes of a raw start tag — "<div style=...>" — the
// way the HTML tokenizer does, but keeping each one's offset, which the
// tokenizer does not report. The value is raw: entities are not decoded, so
// offsets into it are offsets into the source.
func scanAttributes(raw string) []attribute {
	i := 1
	for i < len(raw) && !isSpace(raw[i]) && raw[i] != '>' && raw[i] != '/' {
		i++
	}
	var out []attribute
	for i < len(raw) {
		for i < len(raw) && (isSpace(raw[i]) || raw[i] == '/') {
			i++
		}
		if i >= len(raw) || raw[i] == '>' {
			break
		}
		nameAt := i
		for i < len(raw) && !isSpace(raw[i]) && raw[i] != '=' && raw[i] != '>' && raw[i] != '/' {
			i++
		}
		a := attribute{name: raw[nameAt:i], nameAt: nameAt, valueAt: i}
		j := i
		for j < len(raw) && isSpace(raw[j]) {
			j++
		}
		if j < len(raw) && raw[j] == '=' {
			j++
			for j < len(raw) && isSpace(raw[j]) {
				j++
			}
			if j < len(raw) && (raw[j] == '"' || raw[j] == '\'') {
				quote := raw[j]
				start := j + 1
				end := start
				for end < len(raw) && raw[end] != quote {
					end++
				}
				a.value, a.valueAt = raw[start:end], start
				i = end + 1
			} else {
				start := j
				end := start
				for end < len(raw) && !isSpace(raw[end]) && raw[end] != '>' {
					end++
				}
				a.value, a.valueAt = raw[start:end], start
				i = end
			}
		}
		out = append(out, a)
	}
	return out
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

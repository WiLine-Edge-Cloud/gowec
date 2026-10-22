package wec

import (
	"net/http"
	"regexp"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsID reports whether s looks like a resource id rather than a name.
func IsID(s string) bool { return uuidPattern.MatchString(s) }

// resolve finds want (a name or an id) among items, the way the API resolves
// names on the routes that take them: unknown and ambiguous names are errors
// carrying the names, or the ids, to choose from.
func resolve[T any](items []T, want, kind string, key func(T) (id, name string)) (string, error) {
	if IsID(want) {
		return want, nil
	}
	var names, matches []string
	for _, item := range items {
		id, name := key(item)
		names = append(names, name)
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(want)) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", &APIError{Status: http.StatusNotFound, Code: "unknown_" + kind,
			Message: "No " + kind + " is named " + want, Extra: map[string]any{"available": toAny(names)}}
	default:
		return "", &APIError{Status: http.StatusUnprocessableEntity, Code: "ambiguous_" + kind,
			Message: "More than one " + kind + " is named " + want + "; use its id",
			Extra:   map[string]any{"matches": toAny(matches)}}
	}
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

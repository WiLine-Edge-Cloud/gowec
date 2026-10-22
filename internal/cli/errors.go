package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

// Exit codes.
const (
	ExitOK          = 0
	ExitFailed      = 1
	ExitUsage       = 2
	ExitInterrupted = 130
)

// hints add what to do next to the API's own message.
var hints = map[string]string{
	"missing_key":          "run gowec auth login",
	"invalid_key":          "the key was not recognised; run gowec auth login with a current key",
	"key_revoked":          "create a new key in Settings → API access, then gowec auth login",
	"key_expired":          "create a new key in Settings → API access, then gowec auth login",
	"wrong_environment":    "this key is for another WEC environment; check gowec context list",
	"insufficient_scope":   "the key lacks this permission; an owner can create one that has it",
	"spend_not_allowed":    "the key can't spend from the wallet; create one with Can spend",
	"delete_not_allowed":   "the key can't delete; create one with Can delete",
	"launch_price_changed": "the price moved since the quote; run the command again to see the new one",
	"rate_limited":         "too many requests for this key; wait a moment",
}

// HandleError prints err for a person and returns the exit code it means.
func HandleError(w io.Writer, err error) int {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(w, ui.Warn("interrupted"))
		return ExitInterrupted
	}
	if errors.Is(err, errAborted) {
		fmt.Fprintln(w, ui.Dim.Render(err.Error()))
		return ExitFailed
	}
	var usage *UsageError
	if errors.As(err, &usage) {
		fmt.Fprintln(w, ui.Fail(usage.msg))
		return ExitUsage
	}
	var apiErr *wec.APIError
	if !errors.As(err, &apiErr) {
		fmt.Fprintln(w, ui.Fail(err.Error()))
		return ExitFailed
	}

	fmt.Fprintln(w, ui.Fail(ui.Bold.Render(apiErr.Message)+" "+ui.Dim.Render("("+apiErr.Code+")")))
	for _, f := range apiErr.Fields {
		fmt.Fprintf(w, "  %s %s\n", ui.Yellow.Render(f.Field+":"), f.Message)
	}
	keys := make([]string, 0, len(apiErr.Extra))
	for k := range apiErr.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %s %s\n", ui.Dim.Render(strings.ReplaceAll(k, "_", " ")+":"), extra(apiErr.Extra[k]))
	}
	if hint, ok := hints[apiErr.Code]; ok {
		fmt.Fprintln(w, "  "+ui.Accent.Render("→ ")+hint)
	}
	if apiErr.RequestID != "" {
		fmt.Fprintln(w, ui.Dim.Render("  request "+apiErr.RequestID+" · quote it to support"))
	}
	return ExitFailed
}

func extra(v any) string {
	switch t := v.(type) {
	case []any:
		parts := make([]string, len(t))
		for i, item := range t {
			parts[i] = fmt.Sprint(item)
		}
		return strings.Join(parts, ", ")
	case float64:
		return fmt.Sprintf("%g", t)
	}
	return fmt.Sprint(v)
}

package cli

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

func ticketCmd(r *run) *cobra.Command {
	var since string
	list := listCmd(r, "List the account's tickets (the last 30 days unless --since)",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.Ticket, error) {
			return c.ListTickets(cmd.Context(), since)
		},
		[]string{"NUMBER", "TITLE", "STATE", "REPLIES", "UPDATED", "ID"},
		func(t wec.Ticket) []string {
			replies := ""
			if t.ReplyCount != nil {
				replies = strconv.Itoa(*t.ReplyCount)
			}
			return []string{t.Number, clip(t.Title, 48), ui.State(t.StateName()), dash(replies), day(t.UpdatedAt),
				ui.Dim.Render(strconv.Itoa(t.ID))}
		},
		func(t wec.Ticket) string { return strconv.Itoa(t.ID) })
	list.Flags().StringVar(&since, "since", "", "tickets created on or after this day (YYYY-MM-DD)")
	return group("ticket", "Support tickets", []string{"tickets"},
		list, ticketGetCmd(r), ticketCreateCmd(r), ticketCommentCmd(r))
}

// ticketID reads a ticket id argument ("123" or "#123").
// ticketID reads a ticket argument: its number (as the list and emails show
// it), or its id. A number is looked up among the recent tickets.
func ticketID(cmd *cobra.Command, c *wec.Client, arg string) (int, error) {
	arg = strings.TrimPrefix(arg, "#")
	if tickets, err := c.ListTickets(cmd.Context(), ""); err == nil {
		for _, t := range tickets {
			if t.Number == arg {
				return t.ID, nil
			}
		}
	}
	id, err := strconv.Atoi(arg)
	if err != nil {
		return 0, usagef("%q isn't a ticket number or id: see gowec ticket list", arg)
	}
	return id, nil
}

func ticketArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("ticket", func(ctx context.Context, c *wec.Client) ([]string, error) {
		tickets, err := c.ListTickets(ctx, "")
		numbers := make([]string, len(tickets))
		for i, t := range tickets {
			numbers[i] = t.Number
		}
		return numbers, err
	})
}

func ticketGetCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "get <ticket>",
		Aliases:           []string{"show"},
		Short:             "Show a ticket with its messages (by number or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: ticketArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			id, err := ticketID(cmd, c, args[0])
			if err != nil {
				return err
			}
			t, err := c.GetTicket(cmd.Context(), id)
			if err != nil {
				return err
			}
			return r.show(t, []string{strconv.Itoa(t.ID)}, func() string { return ticketDetail(t) })
		},
	}
}

func ticketDetail(t *wec.Ticket) string {
	var b strings.Builder
	b.WriteString(ui.Pairs([][2]string{
		{"Title", ui.Bold.Render(t.Title)},
		{"Number", t.Number + ui.Dim.Render("  id "+strconv.Itoa(t.ID))},
		{"State", ui.State(t.StateName()) + ui.Dim.Render("  "+t.Priority)},
		{"Opened by", dash(t.OpenedBy)},
		{"Created", ticketTime(t.CreatedAt)},
		{"Updated", ticketTime(t.UpdatedAt)},
	}))
	for _, a := range t.Articles {
		who := a.From
		if who == "" {
			who = a.CreatedBy
		}
		b.WriteString("\n" + ui.Accent.Render(dash(who)) + ui.Dim.Render("  "+a.Sender+" · "+ticketTime(a.CreatedAt)) + "\n")
		for _, line := range strings.Split(articleText(a), "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

// ticketTime is a timestamp to the minute.
func ticketTime(ts string) string {
	if at, ok := parseTime(ts); ok {
		return at.Local().Format("Jan 2, 2006 15:04")
	}
	return dash(ts)
}

var (
	htmlBreak = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|li|tr|h[1-6])>`)
	htmlTag   = regexp.MustCompile(`<[^>]*>`)
	blankRuns = regexp.MustCompile(`\n{3,}`)
)

// articleText is a message's body as plain text; the ticketing system sends
// agents' replies as HTML.
func articleText(a wec.TicketArticle) string {
	body := a.Body
	if strings.Contains(a.ContentType, "html") {
		body = htmlBreak.ReplaceAllString(body, "\n")
		body = html.UnescapeString(htmlTag.ReplaceAllString(body, ""))
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.TrimSpace(blankRuns.ReplaceAllString(body, "\n\n"))
}

func ticketCreateCmd(r *run) *cobra.Command {
	var title, message string
	cmd := &cobra.Command{
		Use:     "create --title <title> --message <text>",
		Aliases: []string{"open", "new"},
		Short:   "Open a ticket with the support team",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			body := wec.NewTicket{Title: title, Article: wec.TicketMessage{Subject: title, Body: message}}
			t, err := c.CreateTicket(cmd.Context(), body, r.idempotencyKey())
			if err != nil {
				return err
			}
			return r.show(t, []string{strconv.Itoa(t.ID)}, func() string {
				return ui.Success(fmt.Sprintf("Opened ticket %s · id %d", t.Number, t.ID)) + "\n" +
					ui.Dim.Render(r.app.Refs(fmt.Sprintf("  follow it with: gowec ticket get %d", t.ID))) + "\n"
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "what the ticket is about, in a line")
	cmd.Flags().StringVar(&message, "message", "", "the message to the support team")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

func ticketCommentCmd(r *run) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:               "comment <ticket> --message <text>",
		Aliases:           []string{"reply"},
		Short:             "Add a message to a ticket (by number or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: ticketArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			id, err := ticketID(cmd, c, args[0])
			if err != nil {
				return err
			}
			out, err := c.CommentTicket(cmd.Context(), id, wec.TicketMessage{Body: message})
			if err != nil {
				return err
			}
			return r.show(out, []string{strconv.Itoa(id)}, func() string {
				return ui.Success(fmt.Sprintf("Added a message to ticket %d", id)) + "\n"
			})
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "the message to add")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

package wec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// Ticket is a support ticket. The ticketing system owns this shape, so only
// the fields the CLI shows are typed; Raw keeps the rest.
type Ticket struct {
	ID          int             `json:"id"`
	Number      string          `json:"number"`
	Title       string          `json:"title"`
	State       string          `json:"state"`
	StateID     int             `json:"state_id"`
	Priority    string          `json:"priority"`
	OpenedBy    string          `json:"opened_by"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	LastReplyAt string          `json:"last_reply_at"`
	ReplyCount  *int            `json:"reply_count"`
	Articles    []TicketArticle `json:"articles"`
	Raw         map[string]any  `json:"-"`
}

// ticketStates are the ticketing system's standard state ids, for rows
// that carry only the id.
var ticketStates = map[int]string{1: "new", 2: "open", 3: "pending reminder", 4: "closed",
	5: "merged", 6: "removed", 7: "pending close"}

// StateName is the ticket's state, named.
func (t *Ticket) StateName() string {
	if t.State != "" {
		return t.State
	}
	return ticketStates[t.StateID]
}

// TicketArticle is one message on a ticket.
type TicketArticle struct {
	ID          int    `json:"id"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	ContentType string `json:"content_type"`
	Sender      string `json:"sender"`
	From        string `json:"from"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	Internal    bool   `json:"internal"`
}

func (t *Ticket) UnmarshalJSON(data []byte) error {
	type plain Ticket
	return keepRaw(data, (*plain)(t), &t.Raw)
}

func (t Ticket) MarshalJSON() ([]byte, error) { return json.Marshal(t.Raw) }

// NewTicket is the body of POST /tickets: a title and the opening message.
type NewTicket struct {
	Title   string        `json:"title"`
	Article TicketMessage `json:"article"`
}

// TicketMessage is a message sent to a ticket.
type TicketMessage struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

func ticketPath(id int) string { return "/tickets/" + strconv.Itoa(id) }

// ListTickets returns the account's tickets created since since (YYYY-MM-DD);
// empty means the API's default, the last 30 days.
func (c *Client) ListTickets(ctx context.Context, since string) ([]Ticket, error) {
	opts := ListOptions{}
	if since != "" {
		opts.Extra = url.Values{"start_date": {since}}
	}
	return listAll[Ticket](ctx, c, "/tickets", opts)
}

// GetTicket reads one of the account's tickets with its messages.
func (c *Client) GetTicket(ctx context.Context, id int) (*Ticket, error) {
	var t Ticket
	return &t, c.get(ctx, ticketPath(id), nil, &t)
}

// CreateTicket opens a ticket. The API needs an idempotency key, so a retry
// never opens it twice.
func (c *Client) CreateTicket(ctx context.Context, body NewTicket, idemKey string) (*Ticket, error) {
	var t Ticket
	err := c.Do(ctx, http.MethodPost, "/tickets", body, &t, &RequestOptions{IdempotencyKey: idemKey})
	return &t, err
}

// CommentTicket adds a message to one of the account's tickets. The route
// takes no idempotency key, so it is sent once, never retried.
func (c *Client) CommentTicket(ctx context.Context, id int, msg TicketMessage) (map[string]any, error) {
	var out map[string]any
	err := c.Do(ctx, http.MethodPost, ticketPath(id)+"/comments", msg, &out, nil)
	return out, err
}

package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

// listCmd builds a "list" action: fetch, then show as a table, JSON or ids.
func listCmd[T any](r *run, short string, fetch func(*cobra.Command, *wec.Client) ([]T, error),
	headers []string, row func(T) []string, id func(T) string) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   short,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			items, err := fetch(cmd, c)
			if err != nil {
				return err
			}
			ids := make([]string, len(items))
			rows := make([][]string, len(items))
			for i, item := range items {
				ids[i], rows[i] = id(item), row(item)
			}
			return r.show(items, ids, func() string {
				if len(items) == 0 {
					return ui.Dim.Render(emptyList(cmd)) + "\n"
				}
				return ui.Table(headers, rows)
			})
		},
	}
}

// zoneFlag adds --zone (name or id) to a catalog list and resolves it to an id.
func zoneFlag(cmd *cobra.Command, zone *string) {
	cmd.Flags().StringVar(zone, "zone", "", "only this zone (name or id)")
}

func zoneID(cmd *cobra.Command, c *wec.Client, zone string) (string, error) {
	if zone == "" {
		return "", nil
	}
	return c.ResolveZoneID(cmd.Context(), zone)
}

func zoneCmd(r *run) *cobra.Command {
	return group("zone", "Where VMs run", []string{"zones"},
		listCmd(r, "List zones",
			func(cmd *cobra.Command, c *wec.Client) ([]wec.Zone, error) { return c.ListZones(cmd.Context()) },
			[]string{"NAME", "ID"},
			func(z wec.Zone) []string { return []string{z.Name, ui.Dim.Render(z.ID)} },
			func(z wec.Zone) string { return z.ID }))
}

func sizeCmd(r *run) *cobra.Command {
	var zone string
	list := listCmd(r, "List VM sizes",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.Size, error) {
			id, err := zoneID(cmd, c, zone)
			if err != nil {
				return nil, err
			}
			return c.ListSizes(cmd.Context(), id)
		},
		[]string{"NAME", "CPU", "MEMORY", "DESCRIPTION"},
		func(s wec.Size) []string {
			return []string{s.Name, fmt.Sprint(s.CPUCount), memory(s.MemoryMB), ui.Dim.Render(s.DisplayText)}
		},
		func(s wec.Size) string { return s.ID })
	zoneFlag(list, &zone)
	return group("size", "VM sizes", []string{"sizes"}, list)
}

func templateCmd(r *run) *cobra.Command {
	var zone string
	list := listCmd(r, "List operating systems",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.Template, error) {
			id, err := zoneID(cmd, c, zone)
			if err != nil {
				return nil, err
			}
			return c.ListTemplates(cmd.Context(), id)
		},
		[]string{"NAME", "OS", "ZONE", "ID"},
		func(t wec.Template) []string {
			return []string{t.Name, dash(t.OSType), zoneName(t.ZoneName), ui.Dim.Render(t.ID)}
		},
		func(t wec.Template) string { return t.ID })
	zoneFlag(list, &zone)
	return group("template", "Operating systems", []string{"templates", "os"}, list)
}

func diskCmd(r *run) *cobra.Command {
	var zone string
	list := listCmd(r, "List root disk sizes",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.Disk, error) {
			id, err := zoneID(cmd, c, zone)
			if err != nil {
				return nil, err
			}
			return c.ListDisks(cmd.Context(), id)
		},
		[]string{"SIZE", "NAME", "ID"},
		func(d wec.Disk) []string {
			size := fmt.Sprintf("%g GB", d.SizeGB)
			if d.CustomSize {
				size = "custom"
			}
			return []string{size, d.Name, ui.Dim.Render(d.ID)}
		},
		func(d wec.Disk) string { return d.ID })
	zoneFlag(list, &zone)
	return group("disk", "Root disk sizes", []string{"disks"}, list)
}

func sshKeyCmd(r *run) *cobra.Command {
	list := listCmd(r, "List SSH keys",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.SSHKey, error) { return c.ListSSHKeys(cmd.Context()) },
		[]string{"NAME", "FINGERPRINT"},
		func(k wec.SSHKey) []string { return []string{k.Name, ui.Dim.Render(k.Fingerprint)} },
		func(k wec.SSHKey) string { return k.Name })

	var name, file string
	add := &cobra.Command{
		Use:   "add --name <name> --public-key-file <path>",
		Short: "Register an SSH public key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(expandHome(file))
			if err != nil {
				return usagef("can't read the public key: %v", err)
			}
			if strings.Contains(string(data), "PRIVATE KEY") {
				return usagef("%s is a private key; give the .pub file", file)
			}
			k, err := c.AddSSHKey(cmd.Context(), name, strings.TrimSpace(string(data)))
			if err != nil {
				return err
			}
			return r.show(k, []string{k.Name}, func() string {
				return ui.Success("Added "+k.Name) + "  " + ui.Dim.Render(k.Fingerprint) + "\n"
			})
		},
	}
	add.Flags().StringVar(&name, "name", "", "the key's name")
	add.Flags().StringVar(&file, "public-key-file", "~/.ssh/id_ed25519.pub", "the public key file")
	_ = add.MarkFlagRequired("name")

	del := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Remove an SSH key (VMs that have it keep it)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if err := r.confirm("Remove the SSH key "+args[0]+"?", "removing a key"); err != nil {
				return err
			}
			if err := c.DeleteSSHKey(cmd.Context(), args[0]); err != nil {
				return err
			}
			return r.show(map[string]string{"name": args[0], "status": "deleted"}, []string{args[0]},
				func() string { return ui.Success("Removed "+args[0]) + "\n" })
		},
	}
	return group("ssh-key", "SSH keys for new VMs", []string{"ssh-keys", "key", "keys"}, list, add, del)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
}

func walletCmd(r *run) *cobra.Command {
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the wallet's balance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			w, err := c.GetWallet(cmd.Context())
			if err != nil {
				return err
			}
			return r.show(w, nil, func() string {
				auto := ui.Dim.Render("off")
				if w.AutoTopupEnabled && w.AutoTopupThreshold != nil && w.AutoTopupAmount != nil {
					auto = fmt.Sprintf("refill to %s below %s", money(*w.AutoTopupAmount, ""), money(*w.AutoTopupThreshold, ""))
				}
				return ui.Pairs([][2]string{
					{"Balance", ui.Bold.Render(money(w.CurrentBalance, ""))},
					{"Added", money(w.TotalEarned, "")},
					{"Spent", money(w.TotalUsed, "")},
					{"Auto top-up", auto},
				})
			})
		},
	}
	var limit int
	tx := listCmd(r, "List the wallet's latest movements",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.WalletTransaction, error) {
			return c.ListWalletTransactions(cmd.Context(), limit)
		},
		[]string{"DATE", "TYPE", "AMOUNT", "BALANCE", "DESCRIPTION"},
		func(t wec.WalletTransaction) []string {
			amount := money(t.Amount, "")
			if t.TransactionType == "used" {
				amount = "-" + amount
			}
			return []string{day(t.CreatedAt), t.TransactionType, amount, money(t.BalanceAfter, ""), ui.Dim.Render(clip(t.Description, 64))}
		},
		func(t wec.WalletTransaction) string { return t.ID })
	tx.Use, tx.Aliases = "transactions", []string{"tx", "history"}
	tx.Flags().IntVar(&limit, "limit", 20, "how many, 1 to 100")
	return group("wallet", "The prepaid wallet", nil, show, tx)
}

// clip shortens s to n characters for a table cell.
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// emptyList says a list came back empty, naming what it lists: "No volumes."
// It takes the resource's plural from its aliases, which list it first.
func emptyList(cmd *cobra.Command) string {
	if parent := cmd.Parent(); parent != nil && len(parent.Aliases) > 0 {
		return "No " + strings.Replace(strings.ReplaceAll(parent.Aliases[0], "-", " "), "ssh", "SSH", 1) + "."
	}
	return "Nothing here."
}

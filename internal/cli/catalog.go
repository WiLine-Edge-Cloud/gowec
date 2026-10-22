package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

func priceCmd(r *run) *cobra.Command {
	list := listCmd(r, "List VM sizes with their prices",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.Price, error) {
			prices, err := c.ListPrices(cmd.Context())
			if err != nil {
				return nil, err
			}
			sort.SliceStable(prices, func(i, j int) bool { return pricePerMonth(prices[i]) < pricePerMonth(prices[j]) })
			return prices, nil
		},
		[]string{"SIZE", "CPU", "MEMORY", "CITY", "A MONTH", "AN HOUR"},
		func(p wec.Price) []string {
			hour := ""
			if p.PricePerHour != nil {
				hour = fmt.Sprintf("$%.4f", *p.PricePerHour)
			}
			return []string{p.Offering.Name, fmt.Sprint(p.Offering.CPUCount), memory(p.Offering.MemoryMB),
				zoneName(p.City), dash(priceCell(p.PricePerMonth)), ui.Dim.Render(dash(hour))}
		},
		func(p wec.Price) string { return p.Offering.ID })
	return group("price", "What VM sizes cost", []string{"prices", "pricing"}, list)
}

func pricePerMonth(p wec.Price) float64 {
	if p.PricePerMonth == nil {
		return 0
	}
	return *p.PricePerMonth
}

func priceCell(v *float64) string {
	if v == nil {
		return ""
	}
	return money(*v, "")
}

func bundleCmd(r *run) *cobra.Command {
	var zone string
	list := listCmd(r, "List VM bundles for sale",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.Bundle, error) {
			city, err := bundleCity(cmd, c, zone)
			if err != nil {
				return nil, err
			}
			return c.ListBundles(cmd.Context(), city)
		},
		[]string{"NAME", "TERM", "PRICE", "SIZE", "DISK", "CITY", "ADD-ONS", "ID"},
		func(b wec.Bundle) []string {
			var addons []string
			for _, a := range b.Addons {
				label := map[string]string{"backup": "backups", "ip_address": "address"}[a.Type]
				if label == "" {
					label = a.Type
				}
				if a.Price != nil {
					label += " +" + money(*a.Price, "")
				}
				addons = append(addons, label)
			}
			return []string{b.Name, fmt.Sprintf("%d mo", b.TermMonths), dash(priceCell(b.PackagePrice)),
				fmt.Sprintf("%dc · %s", b.CPUCount, memory(b.MemoryMB)), fmt.Sprintf("%g GB", b.RootDiskGB),
				zoneName(b.City), dash(strings.Join(addons, ", ")), ui.Dim.Render(b.ID)}
		},
		func(b wec.Bundle) string { return b.ID })
	list.Flags().StringVar(&zone, "zone", "", "only this zone's bundles (name or id)")
	return group("bundle", "VM bundles for sale", []string{"bundles"}, list)
}

// bundleCity is the city bundles are sold in for a zone: its name, lowercased.
func bundleCity(cmd *cobra.Command, c *wec.Client, zone string) (string, error) {
	if zone == "" {
		return "", nil
	}
	zones, err := c.ListZones(cmd.Context())
	if err != nil {
		return "", err
	}
	for _, z := range zones {
		if z.ID == zone || strings.EqualFold(z.Name, zone) {
			return strings.ToLower(z.Name), nil
		}
	}
	return "", usagef("no zone is named %s: see gowec zone list", zone)
}

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
	"github.com/WiLine-Edge-Cloud/gowec/pkg/wec"
)

func jobCmd(r *run) *cobra.Command {
	get := &cobra.Command{
		Use:   "get <job-id>",
		Short: "Show a job's state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			job, err := c.GetJob(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return r.show(job, []string{job.JobID}, func() string { return jobPairs(job) })
		},
	}
	wait := &cobra.Command{
		Use:   "wait <job-id>",
		Short: "Wait for a job to finish",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			r.detach = false
			return r.waitJob(cmd.Context(), c, &wec.JobAccepted{JobID: args[0]}, "Waiting for job "+args[0],
				"job "+args[0]+" succeeded")
		},
	}
	return group("job", "Background actions WEC carries out", []string{"jobs"}, get, wait)
}

func jobPairs(job *wec.Job) string {
	pairs := [][2]string{
		{"Job", job.JobID},
		{"Status", ui.State(job.Status)},
		{"Command", dash(job.Command)},
		{"Resource", dash(strings.TrimSpace(job.ResourceType + " " + job.ResourceID))},
		{"Created", dash(job.Created)},
	}
	if job.ErrorCode != nil {
		pairs = append(pairs, [2]string{"Error", fmt.Sprintf("WEC error %d", *job.ErrorCode)})
	}
	return ui.Pairs(pairs)
}

func launchCmd(r *run) *cobra.Command {
	get := &cobra.Command{
		Use:   "get <launch-id>",
		Short: "Show a launch's progress",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			l, err := c.GetLaunch(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return r.show(l, []string{l.LaunchID}, func() string { return launchPairs(l) })
		},
	}
	wait := &cobra.Command{
		Use:   "wait <launch-id>",
		Short: "Wait for a launch to finish",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			return r.followLaunch(cmd, c, args[0])
		},
	}
	return group("launch", "Prepaid VM launches", []string{"launches"}, get, wait)
}

// followLaunch waits for a launch, showing its step, and prints the machine.
func (r *run) followLaunch(cmd *cobra.Command, c *wec.Client, launchID string) error {
	spin := ui.StartSpinner(r.app.Err, r.tty(), "Launching…")
	l, err := c.WaitLaunch(cmd.Context(), launchID, func(l *wec.Launch) {
		if l.Step != "" {
			spin.Set("Launching · " + strings.ReplaceAll(l.Step, "_", " ") + "…")
		}
	})
	spin.Stop()
	if err != nil {
		return err
	}
	if l.Status == "failed" {
		detail := l.ErrorDetail
		if detail == "" {
			detail = l.ErrorCode
		}
		return fmt.Errorf("the launch failed: %s · launch %s", dash(detail), l.LaunchID)
	}
	return r.show(l, []string{l.VMID}, func() string {
		return ui.Success(dash(l.VMName)+" is up") + "\n\n" + launchPairs(l)
	})
}

func launchPairs(l *wec.Launch) string {
	pairs := [][2]string{
		{"Launch", l.LaunchID},
		{"Status", ui.State(l.Status)},
	}
	if l.Step != "" && !l.Done() {
		pairs = append(pairs, [2]string{"Step", l.Step})
	}
	pairs = append(pairs,
		[2]string{"VM", dash(strings.TrimSpace(l.VMName + "  " + ui.Dim.Render(l.VMID)))},
		[2]string{"Address", dash(l.IPAddress)},
		[2]string{"Plan", dash(l.PlanID)},
	)
	if l.QuotedTotal != nil {
		pairs = append(pairs, [2]string{"Charged", money(*l.QuotedTotal, "")})
	}
	if l.ErrorCode != "" {
		pairs = append(pairs, [2]string{"Error", l.ErrorCode + " " + l.ErrorDetail})
	}
	return ui.Pairs(pairs)
}

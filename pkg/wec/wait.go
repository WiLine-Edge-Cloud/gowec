package wec

import (
	"context"
	"time"
)

const (
	pollFirst = time.Second
	pollMax   = 5 * time.Second
)

// poll calls check until it reports done, backing off from 1s to 5s.
func poll(ctx context.Context, c *Client, check func() (bool, error)) error {
	delay := pollFirst
	for {
		done, err := check()
		if err != nil || done {
			return err
		}
		if err := c.wait(ctx, delay); err != nil {
			return err
		}
		if delay = delay * 3 / 2; delay > pollMax {
			delay = pollMax
		}
	}
}

// WaitJob follows a CloudStack job until it succeeds or fails. onTick, when
// set, sees every state the job is read in.
func (c *Client) WaitJob(ctx context.Context, jobID string, onTick func(*Job)) (*Job, error) {
	var job *Job
	err := poll(ctx, c, func() (bool, error) {
		var err error
		if job, err = c.GetJob(ctx, jobID); err != nil {
			return false, err
		}
		if onTick != nil {
			onTick(job)
		}
		return job.Status != "pending", nil
	})
	return job, err
}

// WaitLaunch follows a prepaid launch until it succeeds or fails.
func (c *Client) WaitLaunch(ctx context.Context, launchID string, onTick func(*Launch)) (*Launch, error) {
	var launch *Launch
	err := poll(ctx, c, func() (bool, error) {
		var err error
		if launch, err = c.GetLaunch(ctx, launchID); err != nil {
			return false, err
		}
		if onTick != nil {
			onTick(launch)
		}
		return launch.Done(), nil
	})
	return launch, err
}

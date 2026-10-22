package wec

import "context"

// Job is a CloudStack job: pending, succeeded or failed.
type Job struct {
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	Command      string `json:"command,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Created      string `json:"created,omitempty"`
	ErrorCode    *int   `json:"error_code,omitempty"`
}

// GetJob reads a job's state.
func (c *Client) GetJob(ctx context.Context, jobID string) (*Job, error) {
	var j Job
	return &j, c.get(ctx, "/jobs/"+PathEscape(jobID), nil, &j)
}

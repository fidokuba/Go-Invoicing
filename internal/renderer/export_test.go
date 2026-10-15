package renderer

import "time"

// SetRetryDelays lets tests shrink the backoff between retries so they
// don't wait out real seconds.
func (c *Client) SetRetryDelays(base, max time.Duration) {
	c.baseDelay = base
	c.maxDelay = max
}

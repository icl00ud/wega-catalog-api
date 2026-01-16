package scraper

import (
	"sync"
	"time"
)

// ProgressTracker tracks scraping progress
type ProgressTracker struct {
	mu sync.RWMutex

	StartedAt        time.Time
	TotalVehicles    int
	Processed        int
	Success          int
	Failed           int
	Skipped          int
	CurrentVehicle   string
	LastError        string

	// Matching stats
	ExactMatch       int
	FuzzyMatch       int
	NoMatch          int

	// Performance
	TotalRequests    int
	NetworkErrors    int
	RateLimitHits    int
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(totalVehicles int) *ProgressTracker {
	return &ProgressTracker{
		StartedAt:     time.Now(),
		TotalVehicles: totalVehicles,
	}
}

// IncrementProcessed increments processed counter
func (p *ProgressTracker) IncrementProcessed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Processed++
}

// IncrementSuccess increments success counter
func (p *ProgressTracker) IncrementSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Success++
}

// IncrementFailed increments failed counter and sets error
func (p *ProgressTracker) IncrementFailed(err string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Failed++
	p.LastError = err
}

// IncrementSkipped increments skipped counter
func (p *ProgressTracker) IncrementSkipped() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Skipped++
}

// IncrementExactMatch increments exact match counter
func (p *ProgressTracker) IncrementExactMatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ExactMatch++
}

// IncrementFuzzyMatch increments fuzzy match counter
func (p *ProgressTracker) IncrementFuzzyMatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FuzzyMatch++
}

// IncrementNoMatch increments no match counter
func (p *ProgressTracker) IncrementNoMatch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.NoMatch++
}

// SetCurrentVehicle sets the current vehicle being processed
func (p *ProgressTracker) SetCurrentVehicle(vehicle string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.CurrentVehicle = vehicle
}

// IncrementRequests increments total requests counter
func (p *ProgressTracker) IncrementRequests() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.TotalRequests++
}

// GetSnapshot returns a snapshot of current progress
func (p *ProgressTracker) GetSnapshot() ProgressSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	elapsed := time.Since(p.StartedAt)
	percentage := 0.0
	if p.TotalVehicles > 0 {
		percentage = (float64(p.Processed) / float64(p.TotalVehicles)) * 100
	}

	// Calculate ETA
	var eta time.Time
	var remaining time.Duration
	if p.Processed > 0 {
		avgTimePerVehicle := elapsed / time.Duration(p.Processed)
		remainingVehicles := p.TotalVehicles - p.Processed
		remaining = avgTimePerVehicle * time.Duration(remainingVehicles)
		eta = time.Now().Add(remaining)
	}

	// Calculate rate
	reqPerSecond := 0.0
	if elapsed.Seconds() > 0 {
		reqPerSecond = float64(p.TotalRequests) / elapsed.Seconds()
	}

	avgTimePerVehicle := 0.0
	if p.Processed > 0 {
		avgTimePerVehicle = elapsed.Seconds() / float64(p.Processed)
	}

	return ProgressSnapshot{
		Status:         "running",
		StartedAt:      p.StartedAt,
		Elapsed:        elapsed,
		TotalVehicles:  p.TotalVehicles,
		Processed:      p.Processed,
		Success:        p.Success,
		Failed:         p.Failed,
		Skipped:        p.Skipped,
		Percentage:     percentage,
		CurrentVehicle: p.CurrentVehicle,
		LastError:      p.LastError,
		ExactMatch:     p.ExactMatch,
		FuzzyMatch:     p.FuzzyMatch,
		NoMatch:        p.NoMatch,
		TotalRequests:  p.TotalRequests,
		RequestsPerSec: reqPerSecond,
		AvgTimePerVehicle: avgTimePerVehicle,
		ETA:            eta,
		Remaining:      remaining,
	}
}

// ProgressSnapshot is a point-in-time snapshot of progress
type ProgressSnapshot struct {
	Status            string
	StartedAt         time.Time
	Elapsed           time.Duration
	TotalVehicles     int
	Processed         int
	Success           int
	Failed            int
	Skipped           int
	Percentage        float64
	CurrentVehicle    string
	LastError         string
	ExactMatch        int
	FuzzyMatch        int
	NoMatch           int
	TotalRequests     int
	RequestsPerSec    float64
	AvgTimePerVehicle float64
	ETA               time.Time
	Remaining         time.Duration
}

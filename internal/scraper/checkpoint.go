package scraper

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Checkpoint represents saved scraper state
type Checkpoint struct {
	LastProcessedID int       `json:"last_processed_id"`
	StartedAt       time.Time `json:"started_at"`
	SavedAt         time.Time `json:"saved_at"`
	Stats           struct {
		Success int `json:"success"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
	} `json:"stats"`
}

// CheckpointManager handles saving and loading scraper state
type CheckpointManager struct {
	filePath string
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager(filePath string) *CheckpointManager {
	return &CheckpointManager{
		filePath: filePath,
	}
}

// Save saves the current checkpoint
func (c *CheckpointManager) Save(lastID int, progress *ProgressTracker) error {
	snapshot := progress.GetSnapshot()

	checkpoint := Checkpoint{
		LastProcessedID: lastID,
		StartedAt:       snapshot.StartedAt,
		SavedAt:         time.Now(),
	}
	checkpoint.Stats.Success = snapshot.Success
	checkpoint.Stats.Failed = snapshot.Failed
	checkpoint.Stats.Skipped = snapshot.Skipped

	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	if err := os.WriteFile(c.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint file: %w", err)
	}

	return nil
}

// Load loads the checkpoint if it exists
func (c *CheckpointManager) Load() (*Checkpoint, error) {
	data, err := os.ReadFile(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No checkpoint exists
		}
		return nil, fmt.Errorf("failed to read checkpoint file: %w", err)
	}

	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}

	return &checkpoint, nil
}

// Delete removes the checkpoint file
func (c *CheckpointManager) Delete() error {
	if err := os.Remove(c.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete checkpoint file: %w", err)
	}
	return nil
}

// Exists checks if checkpoint file exists
func (c *CheckpointManager) Exists() bool {
	_, err := os.Stat(c.filePath)
	return err == nil
}
